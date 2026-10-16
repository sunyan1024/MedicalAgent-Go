package redis

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"GoAI/internal/testutil"
)

func TestSearchErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		message         string
		missing, exists bool
	}{
		{"SEARCH_INDEX_NOT_FOUND Index not found: docs", true, false},
		{"SEARCH_INDEX_EXISTS Index already exists", false, true},
		{"Unknown index name", false, false},
		{"Unknown Index name", false, false},
		{"Index already exists", false, false},
		{"NOAUTH Authentication required", false, false},
		{"connection refused", false, false},
		{"ERR other failure mentioning SEARCH_INDEX_NOT_FOUND", false, false},
	} {
		t.Run(tc.message, func(t *testing.T) {
			err := errors.New(tc.message)
			if isSearchIndexNotFound(err) != tc.missing || isSearchIndexExists(err) != tc.exists {
				t.Fatalf("unexpected classification: %v", err)
			}
		})
	}
	if isSearchIndexNotFound(nil) || isSearchIndexExists(nil) {
		t.Fatal("nil classified as error")
	}
	if !isRediSearchUnsupported(errors.New("ERR unknown command 'FT.INFO', with args beginning with: 'docs'")) {
		t.Fatal("missing Search command not recognized")
	}
}

func TestRedisSearchIndexLifecycle(t *testing.T) {
	client := testutil.RedisSearch(t)
	previous := Rdb
	Rdb = client
	t.Cleanup(func() { Rdb = previous })
	ctx := context.Background()
	key := "redis8102-test"
	if err := client.Do(ctx, "FT.INFO", GenerateIndexName(key)).Err(); !isSearchIndexNotFound(err) {
		t.Fatalf("unexpected missing-index error: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := InitRedisIndex(ctx, key, 3); err != nil {
			t.Fatal(err)
		}
	}
	// Concurrent callers must all succeed even if FT.CREATE races.
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := EnsureChineseKeywordIndex(ctx, client, key)
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if t.Failed() {
		t.FailNow()
	}
	for i := 0; i < 2; i++ {
		if err := DeleteRedisIndex(ctx, key); err != nil {
			t.Fatal(err)
		}
	}
	// Deletion must also succeed when only the vector index exists.
	if err := InitRedisIndex(ctx, key, 3); err != nil {
		t.Fatal(err)
	}
	if err := DeleteRedisIndex(ctx, key); err != nil {
		t.Fatal(err)
	}
	// Invalid schema errors must remain visible.
	if err := InitRedisIndex(ctx, key, 0); err == nil || !strings.Contains(err.Error(), "创建索引失败") {
		t.Fatalf("invalid dimension swallowed: %v", err)
	}
}
