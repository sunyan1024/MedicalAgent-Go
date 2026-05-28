package rag

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestBuildRAGPromptUsesOriginalName(t *testing.T) {
	prompt := BuildRAGPrompt("2023毕业人数是多少", []*schema.Document{
		{
			Content: "毕业生总人数为 1234 人。",
			MetaData: map[string]any{
				"filename":      "47732580096_abc.pdf",
				"original_name": "2023届就业质量报告.pdf",
			},
		},
	})

	if !strings.Contains(prompt, "[2023届就业质量报告.pdf]: 毕业生总人数为 1234 人。") {
		t.Fatalf("prompt does not use original file name: %s", prompt)
	}
	if strings.Contains(prompt, "[文档 1]") {
		t.Fatalf("prompt should not use generic document label when original name exists: %s", prompt)
	}
}

func TestBuildRAGPromptFallsBackAndSkipsNilDocs(t *testing.T) {
	prompt := BuildRAGPrompt("问题", []*schema.Document{
		nil,
		{
			Content: "stored name content",
			MetaData: map[string]any{
				"filename": "stored.pdf",
			},
		},
		{
			Content:  "fallback content",
			MetaData: map[string]any{},
		},
	})

	if !strings.Contains(prompt, "[stored.pdf]: stored name content") {
		t.Fatalf("prompt does not fall back to stored filename: %s", prompt)
	}
	if !strings.Contains(prompt, "[文档 3]: fallback content") {
		t.Fatalf("prompt does not fall back to generic label: %s", prompt)
	}
}

func TestBuildRAGPromptReturnsQueryForEmptyContext(t *testing.T) {
	query := "问题"

	if got := BuildRAGPrompt(query, nil); got != query {
		t.Fatalf("empty docs: got %q, want %q", got, query)
	}
	if got := BuildRAGPrompt(query, []*schema.Document{nil}); got != query {
		t.Fatalf("nil-only docs: got %q, want %q", got, query)
	}
}
