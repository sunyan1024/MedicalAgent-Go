package rag

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	redisPkg "GoAI/common/redis"
	"GoAI/internal/testutil"
	"GoAI/model"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
)

type sourceTestEmbedder struct{}

func (sourceTestEmbedder) EmbedStrings(_ context.Context, texts []string, _ ...embedding.Option) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i := range result {
		result[i] = []float64{1, 0}
	}
	return result, nil
}

func TestSourceMetadataRedisRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := testutil.RedisSearch(t)
	previous := redisPkg.Rdb
	redisPkg.Rdb = client
	t.Cleanup(func() { redisPkg.Rdb = previous })
	key := "stored:source-test"
	if err := redisPkg.InitRedisIndex(ctx, key, 2); err != nil {
		t.Fatal(err)
	}
	idx, err := redisIndexer.NewIndexer(ctx, &redisIndexer.IndexerConfig{
		Client: client, KeyPrefix: redisPkg.GenerateIndexNamePrefix(key), BatchSize: 10,
		Embedding: sourceTestEmbedder{}, DocumentToHashes: documentToHashes,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("原发性肺癌是起源于肺部的恶性肿瘤。", 300)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
	path := filepath.Join(t.TempDir(), "guideline.txt")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	r := &RAGIndexer{indexKey: key, indexer: idx}
	if err := r.IndexFile(ctx, path, hash); err != nil {
		t.Fatal(err)
	}
	for i := range splitTextIntoChunks(content) {
		id := redisPkg.GenerateIndexNamePrefix(key) + fmt.Sprintf("doc_%d", i+1)
		fields, err := client.HGetAll(ctx, id).Result()
		if err != nil || fields["file_sha256"] != hash || fields["chunk_idx"] != fmt.Sprint(i+1) {
			t.Fatalf("incorrect stored metadata for %s: %v", id, err)
		}
	}
	name, err := redisPkg.EnsureChineseKeywordIndex(ctx, client, key)
	if err != nil {
		t.Fatal(err)
	}
	q := &RAGQuery{rdb: client, options: retrieveOptions{keywordTopK: 10}, keywordTargets: []keywordSearchTarget{{indexName: name}}}
	docs, err := q.retrieveKeywordDocuments(ctx, "原发性肺癌")
	if err != nil || len(docs) < 2 {
		t.Fatalf("keyword retrieval: count=%d err=%v", len(docs), err)
	}
	vectorRetriever, err := redisRetriever.NewRetriever(ctx, &redisRetriever.RetrieverConfig{
		Client: client, Index: redisPkg.GenerateIndexName(key), Dialect: 2, TopK: 10,
		VectorField: "vector", Embedding: sourceTestEmbedder{},
		ReturnFields: []string{"content", "metadata", "distance", "file_sha256", "chunk_idx"},
		DocumentConverter: func(_ context.Context, doc redis.Document) (*schema.Document, error) {
			return convertKnowledgeVectorDocument(doc, model.KnowledgeFile{OriginalName: "renamed.txt"}), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	vectorDocs, err := vectorRetriever.Retrieve(ctx, "原发性肺癌")
	if err != nil {
		t.Fatal(err)
	}
	vectors := map[string]*schema.Document{}
	for _, doc := range vectorDocs {
		vectors[doc.ID] = doc
	}

	seen := map[string]bool{}
	for _, doc := range docs {
		want := hash + "_" + doc.MetaData["chunk_idx"].(string)
		vectorDoc := vectors[doc.ID]
		if vectorDoc == nil {
			t.Fatalf("chunk missing from vector retrieval: %s", doc.ID)
		}
		merged := mergeRRFDocuments([]*schema.Document{vectorDoc}, []*schema.Document{doc}, 60)
		if len(merged) != 1 {
			t.Fatalf("same chunk was not merged: %d", len(merged))
		}
		ranked := applyRerankResults(merged, []dashScopeRerankResult{{Index: 0, RelevanceScore: 0.9}})
		for _, candidate := range []*schema.Document{doc, vectorDoc, ranked[0]} {
			got, err := DocumentSourceID(candidate)
			if err != nil || got != want {
				t.Fatalf("source ID: got %q want %q err=%v", got, want, err)
			}
		}
		if seen[want] {
			t.Fatalf("different chunks share ID %s", want)
		}
		seen[want] = true
	}
}

func TestDocumentSourceIDRejectsInvalidMetadata(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, pair := range [][2]string{{"", "1"}, {strings.ToUpper(hash), "1"}, {"abc", "1"}, {hash, ""}, {hash, "0"}, {hash, "-1"}, {hash, "01"}, {hash, "+1"}, {hash, "999999999999999999999999"}} {
		doc := &schema.Document{MetaData: map[string]any{"file_sha256": pair[0], "chunk_idx": pair[1]}}
		if _, err := DocumentSourceID(doc); err == nil {
			t.Fatalf("accepted invalid metadata %v", pair)
		}
	}
	if _, err := DocumentSourceID(nil); err == nil {
		t.Fatal("accepted nil document")
	}
	if err := (&RAGIndexer{}).IndexFile(context.Background(), "unused", "invalid"); err == nil {
		t.Fatal("indexer accepted invalid hash")
	}
}
