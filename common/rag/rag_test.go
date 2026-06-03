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

func TestBuildFileIndexKey(t *testing.T) {
	if got := BuildFileIndexKey("user1", "abc.pdf"); got != "user1:abc.pdf" {
		t.Fatalf("scoped key: got %q", got)
	}
	if got := BuildFileIndexKey("", "fallback.pdf"); got != "fallback.pdf" {
		t.Fatalf("empty username key: got %q", got)
	}
}

func TestBuildStoredFileIndexKey(t *testing.T) {
	if got := BuildStoredFileIndexKey("abc"); got != "stored:abc" {
		t.Fatalf("stored file key: got %q", got)
	}
}

func TestValidateExtractedPDFTextAcceptsReadableText(t *testing.T) {
	text := "OpenClaw 橙皮书主要内容\nPart 1: 初识 OpenClaw\tPart 5: Skills 系统。"

	if err := validateExtractedPDFText(text); err != nil {
		t.Fatalf("readable text should pass: %v", err)
	}
}

func TestValidateExtractedPDFTextRejectsGarbledText(t *testing.T) {
	text := "OpenClaw" + strings.Repeat("\u008c", 3)

	if err := validateExtractedPDFText(text); err == nil {
		t.Fatal("garbled text should fail")
	} else if !strings.Contains(err.Error(), "garbled content ratio too high") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtractedPDFTextRejectsEmptyText(t *testing.T) {
	if err := validateExtractedPDFText(" \n\t "); err == nil {
		t.Fatal("empty text should fail")
	} else if !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtractedPDFTextThresholdBoundary(t *testing.T) {
	atThreshold := strings.Repeat("a", 4) + "\u008c"
	overThreshold := strings.Repeat("a", 3) + "\u008c"

	if err := validateExtractedPDFText(atThreshold); err != nil {
		t.Fatalf("20%% garbled content should pass: %v", err)
	}
	if err := validateExtractedPDFText(overThreshold); err == nil {
		t.Fatal("content over 20% garbled should fail")
	}
}
