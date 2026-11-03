package aihelper

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"GoAI/internal/testutil"
	"GoAI/model"
	"github.com/cloudwego/eino/schema"
)

func TestSummaryInformationBoundaries(t *testing.T) {
	testutil.Config(t)
	raw, err := os.ReadFile("experiments/summary-information-v1/dataset.json")
	if err != nil {
		t.Fatal(err)
	}
	var data informationDataset
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	budget, estimates := informationBudget(data)
	t.Logf("low_budget=%d first_checkpoint_estimates=%v", budget, estimates)
	for _, s := range data.Scenarios {
		t.Run(s.ID, func(t *testing.T) {
			a := NewAIHelper(nil, s.ID)
			store := &summaryFakeStore{}
			a.summaryStore = store
			calls := 0
			position := 0
			a.summarize = func(context.Context, string, []*model.Message) (string, error) {
				calls++
				t.Fatal("default budget must not call summary model for original dataset")
				return "", nil
			}

			for _, cp := range s.Checkpoints {
				for position < cp.Through {
					x := s.Messages[position]
					a.AppendMessage(&model.Message{UID: "synthetic", SessionID: s.ID, StableID: x.ID, Content: x.Content, IsUser: x.Role == "user"}, false)
					position++
				}

				trial := NewAIHelper(nil, s.ID)
				trial.messages = a.GetMessages()
				trial.AppendMessage(&model.Message{UID: "synthetic", IsUser: true, Content: informationQuestion(cp.Question, cp.Fields)}, false)
				trial.summaryStore = store
				trial.summarize = a.summarize
				out, err := trial.ContextMessages(context.Background())
				t.Logf("checkpoint=%d estimated_tokens=%d budget=%d summary_calls=%d", cp.Checkpoint, estimateInputTokens(out), historyTokenBudget, calls)
				if err != nil {
					t.Fatal(err)
				}
				if len(a.GetMessages()) != cp.Through {
					t.Fatal("history modified")
				}
				if len(out) == 0 || out[0].Role == schema.System {
					t.Fatal("short history summarized")
				}

			}
			if calls != 0 || store.saves != 0 {
				t.Fatalf("calls=%d saves=%d", calls, store.saves)
			}
		})
	}
}
