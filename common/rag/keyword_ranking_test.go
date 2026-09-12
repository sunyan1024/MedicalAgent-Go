package rag

import (
	"fmt"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestKeywordRankingKeepsRelevantLateFileInRerankCandidates(t *testing.T) {
	var keywords, vectors []*schema.Document
	for i := 0; i < 70; i++ {
		id := fmt.Sprintf("unrelated-%02d", i)
		keywords = append(keywords, &schema.Document{ID: id, Content: "其他疾病的定义", MetaData: map[string]any{"keyword_retrieved": true, "keyword_rank": i%10 + 1}})
		vectors = append(vectors, &schema.Document{ID: id, Content: "其他疾病的定义", MetaData: map[string]any{"distance": float64(i) / 100}})
	}
	lung := &schema.Document{ID: "lung", Content: "原发性肺癌的定义", MetaData: map[string]any{"keyword_retrieved": true, "keyword_rank": 1}}
	keywords = append(keywords, lung)
	before := mergeRRFDocuments(vectors, keywords, 60)
	for _, doc := range before[:50] {
		if doc.ID == "lung" {
			t.Fatal("fixture must reproduce candidate starvation")
		}
	}
	sortKeywordDocuments(keywords, extractKeywordTerms("原发性肺癌的定义"))
	if keywords[0].ID != "lung" {
		t.Fatal("exact topic should outrank generic definition hits")
	}
	after := prioritizeKeywordCandidates(mergeRRFDocuments(vectors, keywords, 60), 50)
	found := false
	for _, doc := range after[:50] {
		if doc.ID == "lung" {
			found = true
		}
	}
	if !found {
		t.Fatal("relevant late-file hit was dropped before rerank")
	}
	// Reversing file enumeration must not change the keyword ranking.
	for i, j := 0, len(keywords)-1; i < j; i, j = i+1, j-1 {
		keywords[i], keywords[j] = keywords[j], keywords[i]
	}
	sortKeywordDocuments(keywords, extractKeywordTerms("原发性肺癌的定义"))
	if keywords[0].ID != "lung" {
		t.Fatal("keyword ranking depends on file order")
	}
}
