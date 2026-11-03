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
	for _, s := range data.Scenarios {
		t.Run(s.ID, func(t *testing.T) {
			a := NewAIHelper(nil, s.ID)
			store := &summaryFakeStore{}
			a.summaryStore = store
			calls := 0
			position := 0
			a.summarize = func(ctx context.Context, old string, msgs []*model.Message) (string, error) {
				calls++
				if calls > 2 {
					t.Fatal("too many compressions")
				}
				if calls == 1 && old != "" || calls == 2 && old == "" {
					t.Fatal("summary version not reused")
				}
				return "测试摘要", nil
			}
			for _, cp := range s.Checkpoints {
				for position < cp.Through {
					x := s.Messages[position]
					a.AppendMessage(&model.Message{UID: "synthetic", SessionID: s.ID, StableID: x.ID, Content: x.Content, IsUser: x.Role == "user"}, false)
					position++
				}
				if textSize(a.GetMessages()) >= historyLimit {
					t.Fatal("history unexpectedly triggers character limit")
				}
				out, err := a.ContextMessages(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if len(a.GetMessages()) != cp.Through {
					t.Fatal("history modified")
				}
				if len(out) == 0 || out[0].Role != schema.System {
					t.Fatal("summary absent")
				}
				if cp.Checkpoint == 1 && calls != 1 || cp.Checkpoint == 7 && calls != 2 {
					t.Fatalf("wrong compression checkpoint %d: %d", cp.Checkpoint, calls)
				}
			}
			if calls != 2 || store.saves != 2 {
				t.Fatalf("calls=%d saves=%d", calls, store.saves)
			}
		})
	}
}
