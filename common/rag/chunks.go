package rag

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	redisPkg "GoAI/common/redis"
	"GoAI/config"
	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	"github.com/redis/go-redis/v9"
)

var (
	ErrChunkNotFound = errors.New("切片不存在")
	ErrChunkConflict = errors.New("切片已变更或操作已过期，请刷新后重试")
	ErrChunkInvalid  = errors.New("切片参数无效")
)

// Chunk exposes no vector, Redis key, or server file path.
type Chunk struct {
	ID         string `json:"id"`
	ChunkIndex int    `json:"chunk_idx"`
	Content    string `json:"content,omitempty"`
	Summary    string `json:"summary,omitempty"`
	CharCount  int    `json:"char_count"`
	Enabled    bool   `json:"enabled"`
	Version    int    `json:"version"`
	SourceID   string `json:"source_id"`
	Origin     string `json:"origin"`
}

type ChunkPage struct {
	Chunks   []Chunk `json:"chunks"`
	Total    int     `json:"total"`
	Page     int     `json:"page"`
	PageSize int     `json:"page_size"`
}

type ChunkFilter struct {
	Page, PageSize int
	Keyword        string
	Enabled        *bool
}
type ChunkChange struct {
	Content string `json:"content"`
	Enabled *bool  `json:"enabled"`
	Version int    `json:"version"`
}

// ChunkStore is always constructed from an authorized StoredFile, never request keys.
// Callers serialize operations using the shared file lifecycle lock.
type ChunkStore struct {
	Client                       *redis.Client
	IndexKey, FileSHA256, Source string
	Embed                        func(context.Context, string) ([]float64, error)
}

func DisabledChunkPrefix(indexKey string) string { return "rag_chunk_disabled:" + indexKey + ":" }
func ChunkMetaPrefix(indexKey string) string     { return "rag_chunk_meta:" + indexKey + ":" }

func chunkNumber(id string) (int, error) {
	if !strings.HasPrefix(id, "doc_") {
		return 0, ErrChunkInvalid
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, "doc_"))
	if err != nil || n < 1 || n > 1<<31-1 || id != fmt.Sprintf("doc_%d", n) {
		return 0, ErrChunkInvalid
	}
	return n, nil
}

func (s *ChunkStore) read(ctx context.Context, id string) (*Chunk, error) {
	n, err := chunkNumber(id)
	if err != nil {
		return nil, err
	}
	for i, prefix := range []string{redisPkg.GenerateIndexNamePrefix(s.IndexKey), DisabledChunkPrefix(s.IndexKey)} {
		// Read only public fields; vectors and paths never enter the API projection.
		v, err := s.Client.HMGet(ctx, prefix+id, "content", "version", "file_sha256", "origin").Result()
		if err != nil {
			return nil, err
		}
		if v[0] == nil {
			continue
		}
		version := 1
		if v[1] != nil {
			version, err = strconv.Atoi(fmt.Sprint(v[1]))
			if err != nil || version < 1 {
				return nil, fmt.Errorf("invalid chunk version")
			}
		}
		origin := "automatic"
		if v[3] != nil {
			origin = fmt.Sprint(v[3])
		}
		content := fmt.Sprint(v[0])
		return &Chunk{ID: id, ChunkIndex: n, Content: content, CharCount: utf8.RuneCountInString(content), Enabled: i == 0, Version: version, SourceID: fmt.Sprint(v[2]) + "_" + strconv.Itoa(n), Origin: origin}, nil
	}
	return nil, ErrChunkNotFound
}

func (s *ChunkStore) Get(ctx context.Context, id string) (*Chunk, error) { return s.read(ctx, id) }

func (s *ChunkStore) allIDs(ctx context.Context) ([]string, error) {
	seen := map[string]bool{}
	for _, prefix := range []string{redisPkg.GenerateIndexNamePrefix(s.IndexKey), DisabledChunkPrefix(s.IndexKey)} {
		var cursor uint64
		for {
			keys, next, err := s.Client.Scan(ctx, cursor, prefix+"doc_*", 200).Result()
			if err != nil {
				return nil, err
			}
			for _, key := range keys {
				id := strings.TrimPrefix(key, prefix)
				if _, err := chunkNumber(id); err == nil {
					seen[id] = true
				}
			}
			cursor = next
			if cursor == 0 {
				break
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { a, _ := chunkNumber(ids[i]); b, _ := chunkNumber(ids[j]); return a < b })
	return ids, nil
}

func (s *ChunkStore) List(ctx context.Context, f ChunkFilter) (*ChunkPage, error) {
	if f.Page < 1 || f.PageSize < 1 || f.PageSize > 100 || utf8.RuneCountInString(f.Keyword) > 200 {
		return nil, ErrChunkInvalid
	}
	ids, err := s.allIDs(ctx)
	if err != nil {
		return nil, err
	}
	out := &ChunkPage{Chunks: []Chunk{}, Page: f.Page, PageSize: f.PageSize}
	keyword := strings.ToLower(strings.TrimSpace(f.Keyword))
	for _, id := range ids {
		c, err := s.read(ctx, id)
		if err != nil {
			return nil, err
		}
		if f.Enabled != nil && c.Enabled != *f.Enabled || keyword != "" && !strings.Contains(strings.ToLower(c.Content), keyword) {
			continue
		}
		// Division avoids overflow from a maliciously large page parameter.
		if out.Total/f.PageSize == f.Page-1 {
			runes := []rune(c.Content)
			if len(runes) > 160 {
				c.Summary = string(runes[:160]) + "…"
			} else {
				c.Summary = c.Content
			}
			c.Content = ""
			out.Chunks = append(out.Chunks, *c)
		}
		out.Total++
	}
	return out, nil
}

func EmbedManagedChunk(ctx context.Context, content string) ([]float64, error) {
	conf := config.GetConfig().RagModelConfig
	embedder, err := embeddingArk.NewEmbedder(ctx, &embeddingArk.EmbeddingConfig{BaseURL: conf.RagBaseUrl, APIKey: conf.RagApiKey, Model: conf.RagEmbeddingModel})
	if err != nil {
		return nil, err
	}
	vectors, err := embedder.EmbedStrings(ctx, []string{content})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("embedding result count mismatch")
	}
	return vectors[0], nil
}

func (s *ChunkStore) vectorBytes(ctx context.Context, content string) ([]byte, error) {
	embed := s.Embed
	if embed == nil {
		embed = EmbedManagedChunk
	}
	vector, err := embed(ctx, content)
	if err != nil {
		return nil, err
	}
	// Verify against the actual existing index, including when model config changed.
	info, err := s.Client.Do(ctx, "FT.INFO", redisPkg.GenerateIndexName(s.IndexKey)).Slice()
	if err != nil {
		return nil, err
	}
	dimension := 0
	for i := 0; i+1 < len(info); i += 2 {
		if fmt.Sprint(info[i]) == "attributes" {
			if attrs, ok := info[i+1].([]interface{}); ok {
				for _, a := range attrs {
					fields, ok := a.([]interface{})
					if !ok {
						continue
					}
					name := ""
					dim := 0
					for j := 0; j+1 < len(fields); j += 2 {
						switch fmt.Sprint(fields[j]) {
						case "attribute":
							name = fmt.Sprint(fields[j+1])
						case "dim":
							dim, _ = strconv.Atoi(fmt.Sprint(fields[j+1]))
						}
					}
					if name == "vector" {
						dimension = dim
					}
				}
			}
		}
	}
	if dimension == 0 || len(vector) != dimension {
		return nil, fmt.Errorf("embedding dimension does not match index")
	}
	data := make([]byte, 4*len(vector))
	for i, v := range vector {
		f := float32(v)
		if math.IsNaN(v) || math.IsInf(float64(f), 0) {
			return nil, fmt.Errorf("non-finite embedding")
		}
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(f))
	}
	return data, nil
}

// One Lua commit guards both lock ownership and optimistic version. Embedding is
// computed before this script, so failed/expired requests never expose half updates.
var commitChunk = redis.NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return -2 end
local action = ARGV[2]
local active, disabled = KEYS[2], KEYS[3]
local counter = tonumber(redis.call('GET', KEYS[4]) or '0')
local maximum = tonumber(ARGV[9])
if maximum > counter then counter = maximum end
if action == 'create' then
 if counter >= 2147483647 then return -2 end
 local n = counter + 1
 local key = active .. 'doc_' .. n
 if redis.call('EXISTS', key, disabled .. 'doc_' .. n) > 0 then return -2 end
 redis.call('SET', KEYS[4], n)
 redis.call('HSET', key, 'content', ARGV[4], 'vector', ARGV[5], 'version', 1, 'chunk_idx', n, 'file_sha256', ARGV[6], 'metadata', ARGV[7], 'origin', 'manual')
 return n
end
local key = active
if redis.call('EXISTS', key) == 0 then key = disabled end
if redis.call('EXISTS', key) == 0 then return -1 end
local version = tonumber(redis.call('HGET', key, 'version') or '1')
if version ~= tonumber(ARGV[3]) then return -2 end
redis.call('SET', KEYS[4], counter)
if action == 'delete' then redis.call('DEL', key); return 0 end
if action == 'edit' then
 redis.call('HSET', key, 'content', ARGV[4], 'vector', ARGV[5], 'version', version+1, 'origin', 'manual')
else
 local destination = disabled
 if ARGV[8] == '1' then destination = active end
 if destination ~= key then
  if redis.call('EXISTS', destination) ~= 0 then return -2 end
  redis.call('RENAME', key, destination)
 end
 redis.call('HSET', destination, 'version', version+1)
end
return 0
`)

func (s *ChunkStore) Change(ctx context.Context, action, id string, change ChunkChange, lockKey, token string) (*Chunk, error) {
	if action != "create" && action != "edit" && action != "state" && action != "delete" {
		return nil, ErrChunkInvalid
	}
	if action != "create" {
		if _, err := chunkNumber(id); err != nil {
			return nil, err
		}
		if change.Version < 1 {
			return nil, ErrChunkInvalid
		}
		c, err := s.read(ctx, id)
		if err != nil {
			return nil, err
		}
		if c.Version != change.Version {
			return nil, ErrChunkConflict
		}
	}
	if action == "state" && change.Enabled == nil {
		return nil, ErrChunkInvalid
	}
	var vector []byte
	if action == "create" || action == "edit" {
		if !utf8.ValidString(change.Content) || strings.TrimSpace(change.Content) == "" || utf8.RuneCountInString(change.Content) > 10000 {
			return nil, ErrChunkInvalid
		}
		if !fileSHA256Pattern.MatchString(s.FileSHA256) {
			return nil, fmt.Errorf("invalid stored source hash")
		}
		var err error
		vector, err = s.vectorBytes(ctx, change.Content)
		if err != nil {
			return nil, err
		}
	}
	// Initialize high-water mark before ANY mutation, especially deleting the last
	// legacy chunk. Keep the counter even when all chunks have been deleted.
	maximum := 0
	if exists, err := s.Client.Exists(ctx, ChunkMetaPrefix(s.IndexKey)+"sequence").Result(); err != nil {
		return nil, err
	} else if exists == 0 {
		ids, err := s.allIDs(ctx)
		if err != nil {
			return nil, err
		}
		if len(ids) > 0 {
			maximum, _ = chunkNumber(ids[len(ids)-1])
		}
	}
	active, disabled := redisPkg.GenerateIndexNamePrefix(s.IndexKey), DisabledChunkPrefix(s.IndexKey)
	if action != "create" {
		active += id
		disabled += id
	}
	enabled := "0"
	if change.Enabled != nil && *change.Enabled {
		enabled = "1"
	}
	result, err := commitChunk.Run(ctx, s.Client, []string{lockKey, active, disabled, ChunkMetaPrefix(s.IndexKey) + "sequence"}, token, action, change.Version, change.Content, vector, s.FileSHA256, s.Source, enabled, maximum).Int()
	if err != nil {
		return nil, err
	}
	if result == -1 {
		return nil, ErrChunkNotFound
	}
	if result == -2 {
		return nil, ErrChunkConflict
	}
	if action == "delete" {
		return nil, nil
	}
	if action == "create" {
		id = fmt.Sprintf("doc_%d", result)
	}
	return s.read(ctx, id)
}
