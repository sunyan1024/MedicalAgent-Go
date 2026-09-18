package rag

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	"github.com/cloudwego/eino/schema"
)

var fileSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// DocumentSourceID identifies a chunk using the verified original file hash.
// Redis returns both metadata fields as strings; invalid metadata is not replaced
// with a content hash or a Redis key.
func DocumentSourceID(doc *schema.Document) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("missing source document")
	}
	hash, _ := doc.MetaData["file_sha256"].(string)
	if !fileSHA256Pattern.MatchString(hash) {
		return "", fmt.Errorf("invalid file_sha256 for document %q", doc.ID)
	}
	chunk, _ := doc.MetaData["chunk_idx"].(string)
	idx, err := strconv.Atoi(chunk)
	if err != nil || idx < 1 || strconv.Itoa(idx) != chunk {
		return "", fmt.Errorf("invalid chunk_idx for document %q", doc.ID)
	}
	return hash + "_" + chunk, nil
}

func documentToHashes(ctx context.Context, doc *schema.Document) (*redisIndexer.Hashes, error) {
	// 从文档的元数据中取出来源信息（例如文件名、URL）
	source := ""
	if s, ok := doc.MetaData["source"].(string); ok {
		source = s
	}

	// 构造 Redis 中实际存储的数据结构（Hash）
	return &redisIndexer.Hashes{
		// Redis Key 会由 KeyPrefix + doc.ID 组成。
		Key: doc.ID,

		// Redis Hash 中的字段
		Field2Value: map[string]redisIndexer.FieldValue{
			// content：原始文本内容
			// EmbedKey 表示：该字段需要先做向量化，
			// 生成的向量会存入名为 "vector" 的字段中
			"content": {Value: doc.Content, EmbedKey: "vector"},

			// metadata：一些辅助信息，不参与向量计算
			"metadata":    {Value: source},
			"file_sha256": {Value: doc.MetaData["file_sha256"]},
			"chunk_idx":   {Value: doc.MetaData["chunk_idx"]},
		},
	}, nil
}
