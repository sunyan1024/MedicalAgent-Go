package rag

import (
	"context"
	"testing"

	redisPkg "GoAI/common/redis"
	"GoAI/internal/testutil"
)

func TestChineseKeywordRecallExistingHashes(t *testing.T) {
	client := testutil.RedisSearch(t)
	ctx := context.Background()
	file := "stored:lung-guideline"
	prefix := redisPkg.GenerateIndexNamePrefix(file)
	legacy := redisPkg.GenerateIndexName(file)
	if err := client.Do(ctx, "FT.CREATE", legacy, "ON", "HASH", "PREFIX", "1", prefix, "SCHEMA", "content", "TEXT").Err(); err != nil {
		t.Fatal(err)
	}
	content := "原发性肺癌是起源于肺部的恶性肿瘤。"
	if err := client.HSet(ctx, prefix+"1", "content", content, "vector", "unchanged").Err(); err != nil {
		t.Fatal(err)
	}
	if err := client.HSet(ctx, redisPkg.GenerateIndexNamePrefix("stored:private")+"1", "content", content).Err(); err != nil {
		t.Fatal(err)
	}
	question := "什么是原发性肺癌"
	before, err := client.Do(ctx, "FT.SEARCH", legacy, buildRediSearchContentQuery(question)).Result()
	if err != nil {
		t.Fatal(err)
	}
	if redisSearchTotalHits(before) != 0 {
		t.Fatalf("expected to reproduce legacy Chinese recall failure: %v", before)
	}
	name, err := redisPkg.EnsureChineseKeywordIndex(ctx, client, file)
	if err != nil {
		t.Fatal(err)
	}
	r := &RAGQuery{rdb: client, options: retrieveOptions{keywordTopK: 10}, keywordTargets: []keywordSearchTarget{{indexName: name, originalName: "原发性肺癌诊疗指南"}}}
	docs, err := r.retrieveKeywordDocuments(ctx, question)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].ID != prefix+"1" || docs[0].Content != content {
		t.Fatalf("expected only the authorized guideline chunk, got %+v", docs)
	}
	if got := client.HGet(ctx, prefix+"1", "vector").Val(); got != "unchanged" {
		t.Fatalf("existing vector changed: %q", got)
	}
	if again, err := redisPkg.EnsureChineseKeywordIndex(ctx, client, file); err != nil || again != name {
		t.Fatalf("index creation is not idempotent: %q %v", again, err)
	}
}
