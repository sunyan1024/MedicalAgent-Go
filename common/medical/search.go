package medical

import (
	"context"
	"fmt"
	"strings"
	"time"

	"GoAI/common/rag"
)

func PublicSearch(ctx context.Context, query string) ([]Source, error) {
	return searchWithTimeout(ctx, query, func(ctx context.Context, query string) ([]Source, error) {
		q, err := rag.NewMedicalRAGQuery(ctx)
		if err != nil {
			return nil, err
		}
		return retrieveSources(ctx, q, query)
	})
}

func retrieveSources(ctx context.Context, q *rag.RAGQuery, query string) ([]Source, error) {
	docs, err := q.RetrieveDocuments(ctx, query)
	if err != nil {
		return nil, err
	}
	sources := make([]Source, 0, len(docs))
	for _, d := range docs {
		if d == nil {
			continue
		}
		if len(sources) >= 5 {
			break
		}
		title, _ := d.MetaData["original_name"].(string)
		if title == "" {
			title, _ = d.MetaData["filename"].(string)
		}
		if title == "" {
			title = "医学知识资料"
		}
		id, err := rag.DocumentSourceID(d)
		if err != nil {
			return nil, fmt.Errorf("医学知识来源无效: %w", err)
		}
		scope, _ := d.MetaData["knowledge_scope"].(string)
		sources = append(sources, Source{ID: id, Title: title, Content: d.Content, KnowledgeScope: scope})
	}
	return sources, nil
}

// UserSearch binds access to the authenticated user outside model arguments.
func UserSearch(uid string) func(context.Context, string) ([]Source, error) {
	return func(ctx context.Context, query string) ([]Source, error) {
		if uid == "" {
			return nil, fmt.Errorf("用户知识库检索缺少用户身份")
		}
		return searchWithTimeout(ctx, query, func(ctx context.Context, query string) ([]Source, error) {
			q, err := rag.NewRAGQuery(ctx, uid)
			if err != nil {
				return nil, err
			}
			return retrieveSources(ctx, q, query)
		})
	}
}

func searchWithTimeout(ctx context.Context, query string, search func(context.Context, string) ([]Source, error)) ([]Source, error) {
	if strings.TrimSpace(query) == "" || len(query) > 12000 {
		return nil, fmt.Errorf("无效参数 query")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return search(ctx, query)
}
