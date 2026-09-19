package rag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	redisPkg "GoAI/common/redis"
	"GoAI/internal/testutil"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	"github.com/cloudwego/eino/schema"
)

func TestManagedChunksRetrievalAndAtomicChanges(t *testing.T) {
	ctx := context.Background()
	client := testutil.RedisStack(t)
	previous := redisPkg.Rdb
	redisPkg.Rdb = client
	t.Cleanup(func() { redisPkg.Rdb = previous })
	key := "stored:managed-test"
	hash := strings.Repeat("a", 64)
	if err := redisPkg.InitRedisIndex(ctx, key, 2); err != nil {
		t.Fatal(err)
	}
	idx, err := redisIndexer.NewIndexer(ctx, &redisIndexer.IndexerConfig{Client: client, KeyPrefix: redisPkg.GenerateIndexNamePrefix(key), BatchSize: 1, Embedding: sourceTestEmbedder{}, DocumentToHashes: documentToHashes})
	if err != nil {
		t.Fatal(err)
	}
	_, err = idx.Store(ctx, []*schema.Document{{ID: "doc_1", Content: "肺癌诊疗指南", MetaData: map[string]any{"file_sha256": hash, "chunk_idx": "1", "source": "/private/original.txt"}}})
	if err != nil {
		t.Fatal(err)
	}
	keywordIndex, err := redisPkg.EnsureChineseKeywordIndex(ctx, client, key)
	if err != nil {
		t.Fatal(err)
	}
	lockKey, token := "rag:lifecycle:lock:managed-test", "owner"
	if err := client.Set(ctx, lockKey, token, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	store := &ChunkStore{Client: client, IndexKey: key, FileSHA256: hash, Source: "/private/original.txt", Embed: func(context.Context, string) ([]float64, error) { return []float64{0, 1}, nil }}
	original, err := store.Get(ctx, "doc_1")
	if err != nil || original.Version != 1 || !original.Enabled || original.Origin != "automatic" {
		t.Fatalf("legacy read: %+v %v", original, err)
	}
	expectHits := func(want int) {
		t.Helper()
		for _, args := range [][]interface{}{{"FT.SEARCH", keywordIndex, "*", "LIMIT", 0, 100}, {"FT.SEARCH", redisPkg.GenerateIndexName(key), "*=>[KNN 100 @vector $q]", "PARAMS", 2, "q", []byte{0, 0, 128, 63, 0, 0, 0, 0}, "DIALECT", 2}} {
			result, e := client.Do(ctx, args...).Slice()
			if e != nil || fmt.Sprint(result[0]) != fmt.Sprint(want) {
				t.Fatalf("retrieval want %d: %v %v", want, result, e)
			}
		}
	}
	expectHits(1)
	enabled := false
	disabled, err := store.Change(ctx, "state", "doc_1", ChunkChange{Version: 1, Enabled: &enabled}, lockKey, token)
	if err != nil || disabled.Enabled || disabled.Version != 2 {
		t.Fatalf("disable: %+v %v", disabled, err)
	}
	expectHits(0)
	edited, err := store.Change(ctx, "edit", "doc_1", ChunkChange{Version: 2, Content: "哮喘的治疗建议"}, lockKey, token)
	if err != nil || edited.Enabled || edited.Version != 3 || edited.SourceID != hash+"_1" {
		t.Fatalf("disabled edit: %+v %v", edited, err)
	}
	expectHits(0)
	enabled = true
	restored, err := store.Change(ctx, "state", "doc_1", ChunkChange{Version: 3, Enabled: &enabled}, lockKey, token)
	if err != nil || !restored.Enabled {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	expectHits(1)
	q := &RAGQuery{rdb: client, options: retrieveOptions{keywordTopK: 10}, keywordTargets: []keywordSearchTarget{{indexName: keywordIndex}}}
	docs, err := q.retrieveKeywordDocuments(ctx, "哮喘")
	if err != nil || len(docs) != 1 || docs[0].Content != edited.Content {
		t.Fatalf("updated keyword recall: %+v %v", docs, err)
	}
	docs, err = q.retrieveKeywordDocuments(ctx, "肺癌")
	if err != nil || len(docs) != 0 {
		t.Fatalf("stale keyword recall: %+v %v", docs, err)
	}
	active := redisPkg.GenerateIndexNamePrefix(key) + "doc_1"
	if vector := client.HGet(ctx, active, "vector").Val(); vector != string([]byte{0, 0, 0, 0, 0, 0, 128, 63}) {
		t.Fatalf("wrong updated vector: %x", vector)
	}
	before, err := client.HGetAll(ctx, active).Result()
	if err != nil {
		t.Fatal(err)
	}
	for name, embed := range map[string]func(context.Context, string) ([]float64, error){
		"provider failure": func(context.Context, string) ([]float64, error) { return nil, errors.New("provider failed") },
		"wrong dimensions": func(context.Context, string) ([]float64, error) { return []float64{1}, nil },
		"invalid floats":   func(context.Context, string) ([]float64, error) { return []float64{math.NaN(), 0}, nil },
		"float32 overflow": func(context.Context, string) ([]float64, error) { return []float64{math.MaxFloat64, 0}, nil },
	} {
		t.Run(name, func(t *testing.T) {
			store.Embed = embed
			_, err := store.Change(ctx, "edit", "doc_1", ChunkChange{Version: 4, Content: "不应提交"}, lockKey, token)
			if err == nil {
				t.Fatal("accepted failed embedding")
			}
			after := client.HGetAll(ctx, active).Val()
			b, _ := json.Marshal(before)
			a, _ := json.Marshal(after)
			if string(a) != string(b) {
				t.Fatal("failed embedding mutated chunk")
			}
		})
	}
	store.Embed = func(context.Context, string) ([]float64, error) { return []float64{1, 0}, nil }
	if _, err := store.Change(ctx, "edit", "doc_1", ChunkChange{Version: 1, Content: "过期修改"}, lockKey, token); !errors.Is(err, ErrChunkConflict) {
		t.Fatalf("stale version: %v", err)
	}
	// Even an embedder that outlives its lock cannot overwrite a newer owner.
	store.Embed = func(context.Context, string) ([]float64, error) {
		if err := client.Set(ctx, lockKey, "new-owner", time.Minute).Err(); err != nil {
			t.Fatal(err)
		}
		return []float64{1, 0}, nil
	}
	if _, err := store.Change(ctx, "edit", "doc_1", ChunkChange{Version: 4, Content: "失去锁的修改"}, lockKey, token); !errors.Is(err, ErrChunkConflict) {
		t.Fatalf("expired lock: %v", err)
	}
	if got := client.HGet(ctx, active, "content").Val(); got != before["content"] {
		t.Fatal("lost lock changed content")
	}
	if err := client.Set(ctx, lockKey, token, time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	store.Embed = func(context.Context, string) ([]float64, error) { return []float64{1, 0}, nil }
	if _, err := store.Change(ctx, "delete", "doc_1", ChunkChange{Version: 4}, lockKey, token); err != nil {
		t.Fatal(err)
	}
	expectHits(0)
	created, err := store.Change(ctx, "create", "", ChunkChange{Content: "手工添加肺癌资料"}, lockKey, token)
	if err != nil || created.ID != "doc_2" || !created.Enabled || created.Origin != "manual" || created.SourceID != hash+"_2" {
		t.Fatalf("create after delete: %+v %v", created, err)
	}
	expectHits(1)
	// Pagination, body substring search, and public response safety.
	page, err := store.List(ctx, ChunkFilter{Page: 1, PageSize: 1, Keyword: "添加", Enabled: &enabled})
	if err != nil || page.Total != 1 || len(page.Chunks) != 1 || page.Chunks[0].Content != "" {
		t.Fatalf("list: %+v %v", page, err)
	}
	data, _ := json.Marshal(page)
	if strings.Contains(string(data), "/private/") || strings.Contains(string(data), "vector") || strings.Contains(string(data), "rag_docs:") {
		t.Fatalf("unsafe response: %s", data)
	}
	page, err = store.List(ctx, ChunkFilter{Page: 2, PageSize: 1})
	if err != nil || page.Total != 1 || len(page.Chunks) != 0 {
		t.Fatalf("page 2: %+v %v", page, err)
	}
	enabled = false
	if _, err := store.Change(ctx, "state", created.ID, ChunkChange{Version: 1, Enabled: &enabled}, lockKey, token); err != nil {
		t.Fatal(err)
	}
	if err := DeleteIndex(ctx, key); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{redisPkg.GenerateIndexNamePrefix(key), DisabledChunkPrefix(key), ChunkMetaPrefix(key)} {
		keys, _, err := client.Scan(ctx, 0, prefix+"*", 100).Result()
		if err != nil || len(keys) != 0 {
			t.Fatalf("cleanup: %v %v", keys, err)
		}
	}
}

func TestManagedChunkValidationAndLegacySequence(t *testing.T) {
	ctx := context.Background()
	client := testutil.RedisStack(t)
	store := &ChunkStore{Client: client, IndexKey: "stored:legacy"}
	prefix := redisPkg.GenerateIndexNamePrefix(store.IndexKey)
	if err := client.HSet(ctx, prefix+"doc_9", "content", "末尾切片").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.Set(ctx, "lock", "token", time.Minute).Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Change(ctx, "delete", "doc_9", ChunkChange{Version: 1}, "lock", "token"); err != nil {
		t.Fatal(err)
	}
	if got := client.Get(ctx, ChunkMetaPrefix(store.IndexKey)+"sequence").Val(); got != "9" {
		t.Fatalf("lost legacy high water: %s", got)
	}
	for _, id := range []string{"doc_0", "doc_01", "doc_-1", "../doc_1", "doc_2147483648", "doc_1:other", "rag_docs:other:doc_1"} {
		if _, err := store.Get(ctx, id); !errors.Is(err, ErrChunkInvalid) {
			t.Fatalf("accepted ID %q: %v", id, err)
		}
	}
	for _, text := range []string{"", " \n", strings.Repeat("文", 10001), string([]byte{0xff})} {
		if _, err := store.Change(ctx, "create", "", ChunkChange{Content: text}, "lock", "token"); !errors.Is(err, ErrChunkInvalid) {
			t.Fatalf("accepted invalid content: %v", err)
		}
	}
	if _, err := store.Change(ctx, "delete", "doc_9", ChunkChange{Version: 1}, "lock", "token"); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("missing chunk: %v", err)
	}
	if _, err := store.List(ctx, ChunkFilter{Page: 0, PageSize: 20}); !errors.Is(err, ErrChunkInvalid) {
		t.Fatalf("invalid page: %v", err)
	}
}
