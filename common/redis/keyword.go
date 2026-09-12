package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// A separate text index also fixes existing English-tokenized indexes without
// deleting hashes or recomputing embeddings. Redis scans the existing prefix.
func ChineseKeywordIndexName(filename string) string {
	return GenerateIndexName(filename) + ":keyword:zh:v1"
}

func EnsureChineseKeywordIndex(ctx context.Context, client *redis.Client, filename string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("redis client is nil")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	name := ChineseKeywordIndexName(filename)
	info, err := client.Do(ctx, "FT.INFO", name).Result()
	if err != nil {
		if !strings.Contains(err.Error(), "Unknown index name") {
			return "", fmt.Errorf("inspect keyword index: %w", err)
		}
		err = client.Do(ctx, "FT.CREATE", name, "ON", "HASH",
			"PREFIX", "1", GenerateIndexNamePrefix(filename),
			"LANGUAGE", "chinese", "SCHEMA", "content", "TEXT").Err()
		// Another concurrent request may have created the same index.
		if err != nil && !strings.Contains(err.Error(), "Index already exists") {
			return "", fmt.Errorf("create keyword index: %w", err)
		}
		info, err = client.Do(ctx, "FT.INFO", name).Result()
	}
	for {
		if err != nil {
			return "", fmt.Errorf("inspect keyword indexing progress: %w", err)
		}
		values, ok := info.([]interface{})
		if !ok {
			return "", fmt.Errorf("unexpected keyword index info format")
		}
		for i := 0; i+1 < len(values); i += 2 {
			if fmt.Sprint(values[i]) == "indexing" && fmt.Sprint(values[i+1]) == "0" {
				return name, nil
			}
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("wait for keyword index: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
		info, err = client.Do(ctx, "FT.INFO", name).Result()
	}
}
