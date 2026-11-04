package aihelper

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"GoAI/internal/testutil"
	"GoAI/model"
)

// Uses the actual production split, gain acceptance and budget logic with several
// synthetic summary lengths. These simulations do not establish live coverage.
func TestSummaryMedicalRollingBoundaries(t *testing.T) {
	conf := testutil.Config(t)
	t.Logf("live configuration: model=%s thinking=%t", conf.QwenConfig.QwenModelName, conf.QwenConfig.EnableThinking)
	raw, err := os.ReadFile("experiments/summary-medical-rolling-v1/dataset.json")
	if err != nil {
		t.Fatal(err)
	}
	var data informationDataset
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{100, 400, 800} {
		for _, s := range data.Scenarios {
			a := NewAIHelper(nil, s.ID)
			store := &summaryFakeStore{}
			a.summaryStore = store
			a.summarize = func(context.Context, string, []*model.Message) (string, error) {
				return strings.Repeat("摘", size), nil
			}
			position := 0
			for _, cp := range s.Checkpoints {
				for position < cp.Through {
					m := s.Messages[position]
					a.AppendMessage(&model.Message{UID: "synthetic", SessionID: s.ID, StableID: m.ID, Content: m.Content, Role: m.Role}, false)
					position++
				}
				trial := NewAIHelper(nil, s.ID)
				trial.messages = a.GetMessages()
				trial.AppendMessage(&model.Message{UID: "synthetic", Role: "user", Content: informationQuestion(cp.Question, cp.Fields)}, false)
				trial.summary = a.summary
				trial.summaryStore = store
				trial.summarize = a.summarize
				out, err := trial.ContextMessages(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if estimateInputTokens(out) > historyTokenBudget {
					t.Fatal("budget exceeded")
				}
				a.summary = trial.summary
				want := int64(1)
				if cp.Checkpoint >= 5 {
					want = 2
				}
				if cp.Checkpoint >= 9 {
					want = 3
				}
				if a.summary.state.Version != want {
					t.Fatalf("%s size=%d cp=%d version=%d want=%d", s.ID, size, cp.Checkpoint, a.summary.state.Version, want)
				}
			}
		}
	}
}
