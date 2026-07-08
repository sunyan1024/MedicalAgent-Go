package aihelper

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestBuildRAGRewritePromptUsesRecentMessages(t *testing.T) {
	messages := []*schema.Message{
		{Role: schema.User, Content: "第一轮问题"},
		{Role: schema.Assistant, Content: "第一轮回答"},
		{Role: schema.User, Content: "第二轮问题"},
		{Role: schema.Assistant, Content: "第二轮回答"},
		{Role: schema.User, Content: "第三轮问题"},
		{Role: schema.Assistant, Content: "第三轮回答"},
		{Role: schema.User, Content: "那它的就业率呢"},
	}

	prompt := buildRAGRewritePrompt(messages)

	if strings.Contains(prompt, "第一轮问题") {
		t.Fatalf("prompt should only include recent messages: %s", prompt)
	}
	if !strings.Contains(prompt, "那它的就业率呢") {
		t.Fatalf("prompt should include latest question: %s", prompt)
	}
	if !strings.Contains(prompt, "只输出改写后的检索问题") {
		t.Fatalf("prompt should constrain output: %s", prompt)
	}
}

func TestSanitizeRewrittenQueryStripsPrefixesAndQuotes(t *testing.T) {
	got := sanitizeRewrittenQuery("检索问题：\"2023届就业质量报告 就业率\"\n说明：忽略")

	if got != "2023届就业质量报告 就业率" {
		t.Fatalf("sanitized query: got %q", got)
	}
}
