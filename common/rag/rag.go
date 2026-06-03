package rag

import (
	redisPkg "GoAI/common/redis"
	"GoAI/config"
	fileDao "GoAI/dao/file"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	embeddingArk "github.com/cloudwego/eino-ext/components/embedding/ark"
	redisIndexer "github.com/cloudwego/eino-ext/components/indexer/redis"
	redisRetriever "github.com/cloudwego/eino-ext/components/retriever/redis"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	"github.com/ledongthuc/pdf"
	"github.com/redis/go-redis/v9"
)

// RAGIndexer 是一个检索增强生成(RAG)系统的索引器结构体
// embedding 字段用于处理文本嵌入
// indexer 字段是与Redis交互的索引器实例
type RAGIndexer struct {
	embedding embedding.Embedder
	indexer   *redisIndexer.Indexer
}

// RAGQuery 表示一个检索增强生成查询的结构体，包含嵌入器和检索器
// embedding: 用于生成文本嵌入的嵌入器
// retriever: 用于检索相关文档的检索器
type RAGQuery struct {
	embedding  embedding.Embedder
	retrievers []retriever.Retriever
}

func BuildFileIndexKey(username string, storedName string) string {
	if strings.TrimSpace(username) != "" {
		return fmt.Sprintf("%s:%s", username, storedName)
	}
	return storedName
}

func BuildStoredFileIndexKey(storedFileID string) string {
	return fmt.Sprintf("stored:%s", storedFileID)
}

const (
	defaultChunkSize    = 2000
	defaultChunkOverlap = 200
	logPreviewRunes     = 120
	pdfGarbledThreshold = 0.20
)

func previewForLog(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) <= logPreviewRunes {
		return text
	}
	return string(runes[:logPreviewRunes]) + "..."
}

func isGarbledRune(r rune) bool {
	if r == unicode.ReplacementChar {
		return true
	}
	if r >= 0x80 && r <= 0x9f {
		return true
	}
	if unicode.IsControl(r) {
		return true
	}
	return unicode.In(r, unicode.Co, unicode.Cs)
}

func validateExtractedPDFText(text string) error {
	checkedRunes := 0
	garbledRunes := 0

	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		checkedRunes++
		if isGarbledRune(r) {
			garbledRunes++
		}
	}

	if checkedRunes == 0 {
		return fmt.Errorf("failed to extract pdf text: empty content")
	}

	ratio := float64(garbledRunes) / float64(checkedRunes)
	if ratio > pdfGarbledThreshold {
		return fmt.Errorf("failed to extract pdf text: garbled content ratio too high, ratio=%.2f threshold=%.2f", ratio, pdfGarbledThreshold)
	}

	return nil
}

func extractPDFText(filePath string) (string, error) {
	file, reader, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open pdf file: %w", err)
	}
	defer file.Close()

	plainTextReader, err := reader.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to extract pdf text: %w", err)
	}

	content, err := io.ReadAll(plainTextReader)
	if err != nil {
		return "", fmt.Errorf("failed to read pdf text: %w", err)
	}

	text := strings.TrimSpace(string(content))
	if text == "" {
		return "", fmt.Errorf("failed to extract pdf text: empty content")
	}
	if err := validateExtractedPDFText(text); err != nil {
		return "", err
	}

	return text, nil
}

func readIndexableContent(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".pdf" {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read file: %w", err)
		}
		return string(content), nil
	}

	return extractPDFText(filePath)
}

func splitTextIntoChunks(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	runes := []rune(text)
	if len(runes) <= defaultChunkSize {
		return []string{text}
	}

	chunks := make([]string, 0, len(runes)/defaultChunkSize+1)
	for start := 0; start < len(runes); {
		end := start + defaultChunkSize
		if end > len(runes) {
			end = len(runes)
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		if end == len(runes) {
			break
		}

		nextStart := end - defaultChunkOverlap
		if nextStart <= start {
			nextStart = end
		}
		start = nextStart
	}

	return chunks
}

// 构建知识库索引
// 专业说法：文本解析、文本切块、向量化、存储向量
// 通俗理解：把“人能读的文档”，转换成“AI 能按语义搜索的格式”，并存起来
func NewRAGIndexer(filename string) (*RAGIndexer, error) {
	start := time.Now()
	log.Printf("[RAG][Indexer] init start filename=%s", filename)

	// 用于控制整个初始化流程（超时 / 取消等），这里先用默认背景即可
	ctx := context.Background()

	// 读取配置文件
	conf := config.GetConfig()

	// 从环境变量中读取调用向量模型所需的 API Key
	apiKey := conf.RagModelConfig.RagApiKey

	// 向量的维度大小（等于向量模型输出的数字个数）
	// Redis 在创建向量索引时必须提前知道这个值
	dimension := conf.RagModelConfig.RagDimension

	// 1. 配置并创建“向量生成器”（Embedding）
	// 可以理解为：找一个“翻译官”，
	// 专门负责把文本翻译成 AI 能理解的“向量表示”
	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: conf.RagModelConfig.RagBaseUrl,        // 向量模型服务地址
		APIKey:  apiKey,                                // 鉴权信息
		Model:   conf.RagModelConfig.RagEmbeddingModel, // 向量模型
	}

	// 创建向量生成器实例
	// 后续所有文本的“向量化”都会通过它完成
	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		log.Printf("[RAG][Indexer] create embedder failed filename=%s err=%v", filename, err)
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	// 2. 初始化 Redis 中的向量索引结构
	// 可以理解为：先在 Redis 里建好“仓库”，
	// 告诉它以后要存向量，并且每个向量的维度是多少
	if err := redisPkg.InitRedisIndex(ctx, filename, dimension); err != nil {
		log.Printf("[RAG][Indexer] init redis index failed filename=%s dimension=%d err=%v", filename, dimension, err)
		return nil, fmt.Errorf("failed to init redis index: %w", err)
	}

	// 获取 Redis 客户端，用于后续数据写入
	rdb := redisPkg.Rdb

	// 3. 配置索引器（定义：文档如何被存进 Redis）
	indexerConfig := &redisIndexer.IndexerConfig{
		Client:    rdb,                                        // Redis 客户端
		KeyPrefix: redisPkg.GenerateIndexNamePrefix(filename), // 不同知识库使用不同前缀，避免冲突
		BatchSize: 10,                                         // 批量处理文档，提高写入效率

		// 定义：一段文档（Document）在 Redis 中该如何存储
		DocumentToHashes: func(ctx context.Context, doc *schema.Document) (*redisIndexer.Hashes, error) {

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
					"metadata": {Value: source},
				},
			}, nil
		},
	}

	// 将“向量生成器”交给索引器
	// 这样索引器在写入文本时，可以自动完成向量计算
	indexerConfig.Embedding = embedder

	// 4. 创建最终可用的索引器实例
	// 此时索引器已经具备：
	// - 文本 → 向量 的能力
	// - 向量写入 Redis 的能力
	idx, err := redisIndexer.NewIndexer(ctx, indexerConfig)
	if err != nil {
		log.Printf("[RAG][Indexer] create redis indexer failed filename=%s err=%v", filename, err)
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	// 返回一个封装好的 RAGIndexer，
	// 后续只需要调用它，就可以把文档加入知识库
	log.Printf("[RAG][Indexer] init success filename=%s dimension=%d elapsed=%s", filename, dimension, time.Since(start))
	return &RAGIndexer{
		embedding: embedder,
		indexer:   idx,
	}, nil
}

// IndexFile 读取文件内容并创建向量索引
func (r *RAGIndexer) IndexFile(ctx context.Context, filePath string) error {
	start := time.Now()
	log.Printf("[RAG][IndexFile] start file=%s", filePath)

	// 读取文件内容；PDF 会先抽取文本，其余文本文件直接读取。
	content, err := readIndexableContent(filePath)
	if err != nil {
		log.Printf("[RAG][IndexFile] read content failed file=%s err=%v", filePath, err)
		return err
	}
	log.Printf("[RAG][IndexFile] content loaded file=%s bytes=%d", filePath, len(content))

	chunks := splitTextIntoChunks(content)
	if len(chunks) == 0 {
		log.Printf("[RAG][IndexFile] empty content after split file=%s", filePath)
		return fmt.Errorf("failed to index file: empty content")
	}
	log.Printf("[RAG][IndexFile] chunks prepared file=%s chunks=%d chunk_size=%d chunk_overlap=%d", filePath, len(chunks), defaultChunkSize, defaultChunkOverlap)

	docs := make([]*schema.Document, 0, len(chunks))
	for i, chunk := range chunks {
		docs = append(docs, &schema.Document{
			ID:      fmt.Sprintf("doc_%d", i+1),
			Content: chunk,
			MetaData: map[string]any{
				"source": filePath,
			},
		})
	}

	// 使用 indexer 存储文档（会自动进行向量化）
	_, err = r.indexer.Store(ctx, docs)
	if err != nil {
		log.Printf("[RAG][IndexFile] store documents failed file=%s docs=%d err=%v", filePath, len(docs), err)
		return fmt.Errorf("failed to store document: %w", err)
	}

	log.Printf("[RAG][IndexFile] success file=%s docs=%d elapsed=%s", filePath, len(docs), time.Since(start))
	return nil
}

// DeleteIndex 删除指定文件的知识库索引和键值对（静态方法，不依赖实例）
func DeleteIndex(ctx context.Context, filename string) error {
	start := time.Now()
	log.Printf("[RAG][DeleteIndex] start filename=%s", filename)

	if err := redisPkg.DeleteRedisIndex(ctx, filename); err != nil {
		log.Printf("[RAG][DeleteIndex] failed filename=%s err=%v", filename, err)
		return fmt.Errorf("failed to delete redis index: %w", err)
	}
	log.Printf("[RAG][DeleteIndex] success filename=%s elapsed=%s", filename, time.Since(start))
	return nil
}

// NewRAGQuery 创建 RAG 查询器（用于向量检索和问答）
func NewRAGQuery(ctx context.Context, username string) (*RAGQuery, error) {
	start := time.Now()
	log.Printf("[RAG][Query] init start username=%s", username)

	conf := config.GetConfig()
	apiKey := conf.RagModelConfig.RagApiKey

	// 创建 embedding 模型
	embedConfig := &embeddingArk.EmbeddingConfig{
		BaseURL: conf.RagModelConfig.RagBaseUrl,
		APIKey:  apiKey,
		Model:   conf.RagModelConfig.RagEmbeddingModel,
	}
	embedder, err := embeddingArk.NewEmbedder(ctx, embedConfig)
	if err != nil {
		log.Printf("[RAG][Query] create embedder failed username=%s err=%v", username, err)
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}

	knowledgeFiles, err := fileDao.GetKnowledgeFilesByUserName(username)
	if err != nil {
		log.Printf("[RAG][Query] load knowledge files failed username=%s err=%v", username, err)
		return nil, err
	}
	if len(knowledgeFiles) == 0 {
		log.Printf("[RAG][Query] no uploaded files username=%s", username)
		return nil, fmt.Errorf("no uploaded file found for user %s", username)
	}
	log.Printf("[RAG][Query] knowledge files loaded username=%s files=%d", username, len(knowledgeFiles))

	rdb := redisPkg.Rdb
	retrievers := make([]retriever.Retriever, 0, len(knowledgeFiles))
	for _, knowledgeFile := range knowledgeFiles {
		filename := strings.TrimSpace(knowledgeFile.StoredName)
		storedFileID := strings.TrimSpace(knowledgeFile.StoredFileID)
		if filename == "" || storedFileID == "" {
			continue
		}
		originalName := strings.TrimSpace(knowledgeFile.OriginalName)
		indexKey := BuildStoredFileIndexKey(storedFileID)
		indexName := redisPkg.GenerateIndexName(indexKey)
		retrieverConfig := &redisRetriever.RetrieverConfig{
			Client:       rdb,
			Index:        indexName,
			Dialect:      2,
			ReturnFields: []string{"content", "metadata", "distance"},
			TopK:         5,
			VectorField:  "vector",
			// 由于 Redis 存储的文档格式和 Eino 框架内部定义的 schema.Document 格式不同
			// 因此在查询时需要通过 DocumentConverter 进行相应的转换。
			DocumentConverter: func(ctx context.Context, doc redis.Document) (*schema.Document, error) {
				resp := &schema.Document{
					ID:       doc.ID,
					Content:  "",
					MetaData: map[string]any{},
				}
				for field, val := range doc.Fields {
					if field == "content" {
						resp.Content = val
					} else {
						resp.MetaData[field] = val
					}
				}
				resp.MetaData["filename"] = filename
				if originalName != "" {
					resp.MetaData["original_name"] = originalName
				}
				return resp, nil
			},
		}
		retrieverConfig.Embedding = embedder

		rtr, err := redisRetriever.NewRetriever(ctx, retrieverConfig)
		if err != nil {
			log.Printf("[RAG][Query] create retriever failed username=%s file=%s index=%s err=%v", username, filename, indexName, err)
			continue
		}
		retrievers = append(retrievers, rtr)
		log.Printf("[RAG][Query] retriever ready username=%s file=%s original_name=%s index=%s", username, filename, originalName, indexName)
	}

	if len(retrievers) == 0 {
		log.Printf("[RAG][Query] no valid retriever username=%s elapsed=%s", username, time.Since(start))
		return nil, fmt.Errorf("no valid file index found for user %s", username)
	}

	log.Printf("[RAG][Query] init success username=%s retrievers=%d elapsed=%s", username, len(retrievers), time.Since(start))
	return &RAGQuery{
		embedding:  embedder,
		retrievers: retrievers,
	}, nil
}

// RetrieveDocuments 检索相关文档
func (r *RAGQuery) RetrieveDocuments(ctx context.Context, query string) ([]*schema.Document, error) {
	start := time.Now()
	log.Printf("[RAG][Retrieve] start retrievers=%d query_len=%d query_preview=%q", len(r.retrievers), len(query), previewForLog(query))

	var allDocs []*schema.Document
	for i, rtr := range r.retrievers {
		retrieveStart := time.Now()
		docs, err := rtr.Retrieve(ctx, query)
		if err != nil {
			log.Printf("[RAG][Retrieve] retriever failed index=%d err=%v elapsed=%s", i, err, time.Since(retrieveStart))
			continue
		}
		log.Printf("[RAG][Retrieve] retriever success index=%d docs=%d elapsed=%s", i, len(docs), time.Since(retrieveStart))
		allDocs = append(allDocs, docs...)
	}

	if len(allDocs) == 0 {
		log.Printf("[RAG][Retrieve] no documents retrieved retrievers=%d elapsed=%s", len(r.retrievers), time.Since(start))
		return nil, fmt.Errorf("failed to retrieve documents")
	}

	sort.SliceStable(allDocs, func(i, j int) bool {
		return documentDistance(allDocs[i]) < documentDistance(allDocs[j])
	})

	if len(allDocs) > 5 {
		allDocs = allDocs[:5]
	}
	for i, doc := range allDocs {
		docID := ""
		filename := ""
		originalName := ""
		contentLen := 0
		distance := documentDistance(doc)
		if doc == nil {
			log.Printf("[RAG][Retrieve] hit rank=%d id=%s filename=%s original_name=%s distance=%.6f content_len=%d", i+1, docID, filename, originalName, distance, contentLen)
			continue
		}
		docID = doc.ID
		contentLen = len(doc.Content)
		if doc.MetaData != nil {
			if v, ok := doc.MetaData["filename"].(string); ok {
				filename = v
			}
			if v, ok := doc.MetaData["original_name"].(string); ok {
				originalName = v
			}
		}
		log.Printf("[RAG][Retrieve] hit rank=%d id=%s filename=%s original_name=%s distance=%.6f content_len=%d", i+1, docID, filename, originalName, distance, contentLen)
	}
	log.Printf("[RAG][Retrieve] success docs=%d elapsed=%s", len(allDocs), time.Since(start))
	return allDocs, nil
}

func documentDistance(doc *schema.Document) float64 {
	if doc == nil || doc.MetaData == nil {
		return 1
	}

	val, ok := doc.MetaData["distance"]
	if !ok {
		return 1
	}

	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case string:
		distance, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return distance
		}
	}

	return 1
}

func ragDocumentLabel(doc *schema.Document, fallbackIndex int) string {
	if doc != nil && doc.MetaData != nil {
		if originalName, ok := doc.MetaData["original_name"].(string); ok {
			originalName = strings.TrimSpace(originalName)
			if originalName != "" {
				return originalName
			}
		}
		if filename, ok := doc.MetaData["filename"].(string); ok {
			filename = strings.TrimSpace(filename)
			if filename != "" {
				return filename
			}
		}
	}

	return fmt.Sprintf("文档 %d", fallbackIndex)
}

// BuildRAGPrompt 构建包含检索文档的提示词
func BuildRAGPrompt(query string, docs []*schema.Document) string {
	if len(docs) == 0 {
		return query
	}

	var contextText strings.Builder
	for i, doc := range docs {
		if doc == nil {
			continue
		}
		label := ragDocumentLabel(doc, i+1)
		contextText.WriteString(fmt.Sprintf("[%s]: %s\n\n", label, doc.Content))
	}

	if contextText.Len() == 0 {
		return query
	}

	prompt := fmt.Sprintf(`基于以下参考文档回答用户的问题。如果文档中没有相关信息，请说明无法找到相关信息。回答中引用资料时，请使用参考文档标题中的文件名。

参考文档：
%s

用户问题：%s

请提供准确、完整的回答：`, contextText.String(), query)

	return prompt
}
