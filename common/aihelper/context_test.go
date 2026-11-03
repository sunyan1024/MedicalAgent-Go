package aihelper

import (
	"GoAI/common/mysql"
	"GoAI/model"
	"context"
	"strings"
	"testing"
)

func TestContextWindowPreservesArchive(t *testing.T) {
	old := mysql.DB
	mysql.DB = nil
	defer func() { mysql.DB = old }()
	a := NewAIHelper(nil, "test")
	for i := 0; i < 16; i++ {
		a.AppendMessage(&model.Message{Content: "question", IsUser: true}, false)
		a.AppendMessage(&model.Message{Content: "answer"}, false)
	}
	a.AppendMessage(&model.Message{Content: "current", IsUser: true}, false)
	msgs, err := a.ContextMessages(context.Background())
	if err != nil || len(msgs) != 33 || msgs[len(msgs)-1].Content != "current" {
		t.Fatalf("window: %d %v", len(msgs), err)
	}
	if len(a.GetMessages()) != 33 {
		t.Fatal("archive truncated")
	}
}
func TestContextLengthFallbackAndOversizedQuestion(t *testing.T) {
	old := mysql.DB
	mysql.DB = nil
	defer func() { mysql.DB = old }()
	a := NewAIHelper(nil, "test")
	a.AppendMessage(&model.Message{Content: strings.Repeat("长", 25000), IsUser: true}, false)
	a.AppendMessage(&model.Message{Content: "answer"}, false)
	a.AppendMessage(&model.Message{Content: "current", IsUser: true}, false)
	msgs, err := a.ContextMessages(context.Background())
	if err != nil || len(msgs) != 1 {
		t.Fatalf("fallback: %d %v", len(msgs), err)
	}
	a.AppendMessage(&model.Message{Content: strings.Repeat("长", 25000), IsUser: true}, false)
	if _, err = a.ContextMessages(context.Background()); err == nil {
		t.Fatal("oversized question accepted")
	}
}
