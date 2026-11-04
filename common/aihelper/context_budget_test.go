package aihelper

import (
	"context"
	"strings"
	"testing"

	"GoAI/model"
	"github.com/cloudwego/eino/schema"
)

func TestEstimatedTokens(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{{"", 8}, {"abcde", 10}, {"a中🙂", 13}} {
		if got := estimateMessageTokens(tc.text); got != tc.want {
			t.Fatalf("%q: %d want %d", tc.text, got, tc.want)
		}
	}
	if estimateInputTokens([]*schema.Message{schema.UserMessage("abcd")}) != 12 {
		t.Fatal("missing format overhead")
	}
}

func TestBudgetCompleteTurnsAndSingleSummary(t *testing.T) {
	a, s := summaryHistory(t, 16)
	calls := 0
	a.summarize = func(_ context.Context, _ string, msgs []*model.Message) (string, error) {
		calls++
		if len(msgs)%2 != 0 || msgs[0].Role != "user" || msgs[len(msgs)-1].Role == "user" {
			t.Fatal("partial turn")
		}
		return "简短事实", nil
	}
	out, err := a.ContextMessages(context.Background())
	if err != nil || calls != 1 || s.state.Version != 1 || estimateInputTokens(out) > historyTokenBudget {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	if out[len(out)-1].Content != "current" || len(a.GetMessages()) != 33 {
		t.Fatal("current/archive changed")
	}
}

func TestCompressionTargetIsNotAcceptanceLimit(t *testing.T) {
	// With introduction overhead these candidates are between 50% and 70%,
	// and above 70%, respectively. More than 2000 characters is permitted.
	for _, tc := range []struct {
		size   int
		accept bool
	}{{2400, true}, {3300, false}} {
		a := NewAIHelper(nil, "gain")
		s := &summaryFakeStore{}
		a.summaryStore = s
		a.AppendMessage(&model.Message{UID: "u", Role: "user", Content: strings.Repeat("x", 16000)}, false)
		a.AppendMessage(&model.Message{UID: "u", Content: "answer", Role: "assistant"}, false)
		a.AppendMessage(&model.Message{UID: "u", Role: "user", Content: strings.Repeat("x", 8500)}, false)
		a.summarize = func(context.Context, string, []*model.Message) (string, error) {
			return strings.Repeat("x", tc.size*4), nil
		}
		// Use a candidate token count that is 60% or 82% of replaced text.
		out, err := a.ContextMessages(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if (s.state.Version == 1) != tc.accept {
			t.Fatalf("size=%d state=%+v", tc.size, s.state)
		}
		if estimateInputTokens(out) > historyTokenBudget {
			t.Fatal("over budget")
		}
		if !tc.accept && (s.saves != 0 || a.summary.state.Through != "") {
			t.Fatal("rejected summary advanced")
		}
	}
}

func TestQuestionAndOldSummaryBudgetProtection(t *testing.T) {
	a, s := summaryHistory(t, 0)
	a.GetMessages()[0].Content = strings.Repeat("中", 3000)
	if _, err := a.ContextMessages(context.Background()); err == nil {
		t.Fatal("token-oversized current accepted")
	}
	a.GetMessages()[0].Content = strings.Repeat("x", 24001)
	if _, err := a.contextMessagesWithBudget(context.Background(), 10000); err == nil {
		t.Fatal("character guard lost")
	}
	a.GetMessages()[0].Content = "current"
	s.state = summaryState{Text: strings.Repeat("中", 3000), Version: 2}
	if _, err := a.ContextMessages(context.Background()); err == nil {
		t.Fatal("summary silently truncated")
	}
}

func TestFallbackRecordsUncoveredCompleteTurns(t *testing.T) {
	a, s := summaryHistory(t, 16)
	s.pending = true
	var trim map[string]any
	out, err := a.contextMessagesWithBudget(context.Background(), historyTokenBudget, func(row map[string]any) {
		if row["kind"] == "trimmed_uncovered" {
			trim = row
		}
	})
	if err != nil || trim == nil || trim["end"].(int)%2 != 0 || estimateInputTokens(out) > historyTokenBudget || s.state.Version != 0 {
		t.Fatalf("trim=%v err=%v", trim, err)
	}
	if a.GetMessages()[trim["end"].(int)].Role != "user" {
		t.Fatal("partial fallback")
	}
}

func TestRejectSummaryThatExceedsCombinedBudget(t *testing.T) {
	a := NewAIHelper(nil, "combined")
	s := &summaryFakeStore{}
	a.summaryStore = s
	for _, m := range []*model.Message{
		{UID: "u", Role: "user", Content: strings.Repeat("x", 16000)},
		{UID: "u", Content: "answer"},
		{UID: "u", Role: "user", Content: strings.Repeat("x", 18000)},
	} {
		a.AppendMessage(m, false)
	}
	calls := 0
	a.summarize = func(context.Context, string, []*model.Message) (string, error) {
		calls++
		return strings.Repeat("x", 8000), nil
	}
	status := ""
	out, err := a.contextMessagesWithBudget(context.Background(), historyTokenBudget, func(row map[string]any) {
		if row["kind"] == "summary_decision" {
			status = row["status"].(string)
		}
	})
	if err != nil || calls != 1 || status != "rejected_budget" || s.saves != 0 || s.state.Version != 0 || len(out) != 1 {
		t.Fatalf("calls=%d status=%s err=%v", calls, status, err)
	}
}

func TestFailedCandidateNotRegenerated(t *testing.T) {
	a, _ := summaryHistory(t, 16)
	calls := 0
	a.summarize = func(context.Context, string, []*model.Message) (string, error) {
		calls++
		return strings.Repeat("中", 20000), nil
	}
	for i := 0; i < 2; i++ {
		if _, err := a.ContextMessages(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("same failed candidate generated %d times", calls)
	}
	a.AppendMessage(&model.Message{UID: "alice", Content: "answer", Role: "assistant"}, false)
	a.AppendMessage(&model.Message{UID: "alice", Role: "user", Content: "small new question"}, false)
	a.ContextMessages(context.Background())
	if calls != 1 {
		t.Fatal("retried before enough new history")
	}
}
