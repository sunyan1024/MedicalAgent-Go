package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
)

const (
	defaultRerankBaseURL        = "https://dashscope.aliyuncs.com"
	defaultRerankModel          = "gte-rerank-v2"
	defaultRerankCandidateTopK  = 10
	defaultRerankTopK           = 5
	defaultRerankTimeoutSeconds = 15
	dashScopeRerankPath         = "/api/v1/services/rerank/text-rerank/text-rerank"
)

type documentReranker interface {
	Rerank(ctx context.Context, query string, docs []*schema.Document, topK int) ([]*schema.Document, error)
}

type dashScopeReranker struct {
	baseURL    string
	apiKey     string
	model      string
	httpClient *http.Client
}

type dashScopeRerankRequest struct {
	Model      string                    `json:"model"`
	Input      dashScopeRerankInput      `json:"input"`
	Parameters dashScopeRerankParameters `json:"parameters,omitempty"`
}

type dashScopeRerankInput struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type dashScopeRerankParameters struct {
	TopN            int  `json:"top_n,omitempty"`
	ReturnDocuments bool `json:"return_documents"`
}

type dashScopeRerankResponse struct {
	Output dashScopeRerankOutput `json:"output"`
}

type dashScopeRerankOutput struct {
	Results []dashScopeRerankResult `json:"results"`
}

type dashScopeRerankResult struct {
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

func newDashScopeReranker(baseURL, apiKey, model string, timeout time.Duration) (*dashScopeReranker, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultRerankBaseURL
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid rerank base url %q: %w", baseURL, err)
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("rerank api key cannot be empty")
	}

	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultRerankModel
	}

	if timeout <= 0 {
		timeout = defaultRerankTimeoutSeconds * time.Second
	}

	return &dashScopeReranker{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (r *dashScopeReranker) Rerank(ctx context.Context, query string, docs []*schema.Document, topK int) ([]*schema.Document, error) {
	docs = nonNilDocuments(docs)
	if len(docs) == 0 {
		return nil, fmt.Errorf("rerank documents cannot be empty")
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("rerank query cannot be empty")
	}

	documents := make([]string, 0, len(docs))
	for _, doc := range docs {
		documents = append(documents, doc.Content)
	}
	if topK <= 0 || topK > len(documents) {
		topK = len(documents)
	}

	body, err := json.Marshal(dashScopeRerankRequest{
		Model: r.model,
		Input: dashScopeRerankInput{
			Query:     query,
			Documents: documents,
		},
		Parameters: dashScopeRerankParameters{
			TopN:            topK,
			ReturnDocuments: false,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+dashScopeRerankPath, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+r.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send rerank request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read rerank response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("rerank request failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded dashScopeRerankResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}

	reranked := applyRerankResults(docs, decoded.Output.Results)
	if len(reranked) == 0 {
		return nil, fmt.Errorf("rerank response has no valid results")
	}
	return reranked, nil
}

func applyRerankResults(docs []*schema.Document, results []dashScopeRerankResult) []*schema.Document {
	docs = nonNilDocuments(docs)
	if len(docs) == 0 || len(results) == 0 {
		return nil
	}

	results = append([]dashScopeRerankResult(nil), results...)
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].RelevanceScore > results[j].RelevanceScore
	})

	used := make(map[int]bool, len(results))
	reranked := make([]*schema.Document, 0, len(docs))
	for _, result := range results {
		if result.Index < 0 || result.Index >= len(docs) || used[result.Index] {
			continue
		}
		doc := docs[result.Index]
		if doc.MetaData == nil {
			doc.MetaData = map[string]any{}
		}
		doc.MetaData["rerank_score"] = result.RelevanceScore
		reranked = append(reranked, doc)
		used[result.Index] = true
	}

	for i, doc := range docs {
		if !used[i] {
			reranked = append(reranked, doc)
		}
	}
	return reranked
}

func nonNilDocuments(docs []*schema.Document) []*schema.Document {
	filtered := make([]*schema.Document, 0, len(docs))
	for _, doc := range docs {
		if doc != nil {
			filtered = append(filtered, doc)
		}
	}
	return filtered
}

func rerankScore(doc *schema.Document) (float64, bool) {
	if doc == nil || doc.MetaData == nil {
		return 0, false
	}
	val, ok := doc.MetaData["rerank_score"]
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case string:
		score, err := strconv.ParseFloat(v, 64)
		return score, err == nil
	default:
		return 0, false
	}
}
