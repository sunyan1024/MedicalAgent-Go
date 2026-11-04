package aihelper

import (
	"GoAI/config"
	"GoAI/internal/testutil"
	"GoAI/model"
	"GoAI/utils"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/schema"
)

// Opt-in synthetic experiment. Never connects to the application database.
func TestSummaryLiveComparison(t *testing.T) {
	if os.Getenv("SUMMARY_LIVE") != "1" {
		t.Skip("set SUMMARY_LIVE=1 and GOPHERAI_TEST_MYSQL=1")
	}
	db := testutil.MySQL(t)
	if err := db.AutoMigrate(&model.Session{}, &model.Message{}); err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("SUMMARY_RESULTS")
	if dir == "" {
		dir = "/tmp/summary-comparison"
	}
	os.MkdirAll(dir, 0755)
	f, err := os.Create(filepath.Join(dir, "samples.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	conf := config.GetConfig()
	meta, _ := json.MarshalIndent(map[string]any{"model": conf.QwenConfig.QwenModelName, "thinking": conf.QwenConfig.EnableThinking, "started_at": time.Now().UTC(), "note": "synthetic data; summary usage observed; fixed replay; no warmup"}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), meta, 0644); err != nil {
		t.Fatal(err)
	}
	calls := 0
	failures := 0
	consecutiveFailures := 0
	for scenario, turns := range []int{16, 12} {
		for repeat := 0; repeat < 1; repeat++ {
			sid := fmt.Sprintf("experiment-%d-%d", scenario, repeat)
			a := NewAIHelper(nil, sid)
			a.summaryStore = mysqlSummaryRepository{db: db}
			db.Create(&model.Session{ID: sid, UID: "synthetic"})
			filler := 450
			if scenario == 1 {
				filler = 750
			}
			if scenario == 1 {
				filler = 2500
			}
			for i := 0; i < turns; i++ {
				content := fmt.Sprintf("第%d轮。", i) + strings.Repeat("这是虚构的偏好讨论。", filler/12)
				if i == 0 {
					content += fmt.Sprintf("记录对象是人物%d；发生时间2026-01-03；过敏信息青霉素；偏好BLUE；待办预约；没有明确诊断。", scenario)
				}
				if i == 2 {
					content += "更正：偏好GREEN，之前BLUE作废。"
				}
				a.AppendMessage(&model.Message{UID: "synthetic", SessionID: sid, Role: "user", Content: content}, false)
				a.AppendMessage(&model.Message{UID: "synthetic", SessionID: sid, Content: "已记录。", Role: "assistant"}, false)
			}
			rounds := 10
			if scenario == 3 {
				rounds = 3
			}
			for round := 0; round < rounds; round++ {
				if round == 4 {
					a.AppendMessage(&model.Message{UID: "synthetic", SessionID: sid, Role: "user", Content: "更正：最新偏好YELLOW，GREEN作废。"}, false)
					a.AppendMessage(&model.Message{UID: "synthetic", SessionID: sid, Content: "已记录更正。", Role: "assistant"}, false)
				}
				a.AppendMessage(&model.Message{UID: "synthetic", SessionID: sid, Role: "user", Content: "只输出JSON，字段person、onset、allergy、preference、pending、diagnosis，分别为记录对象、发生时间、过敏信息、最新偏好、待办、明确诊断。无明确依据填unknown。"}, false)
				all := a.GetMessages()
				for _, msg := range all {
					if msg.ID == 0 {
						if e := db.Create(msg).Error; e != nil {
							t.Fatal(e)
						}
					}
				}
				started := time.Now()
				summaryTokens := 0
				summaryOutput := 0
				a.summarize = func(ctx context.Context, old string, msgs []*model.Message) (string, error) {
					summaryTokens = -1
					summaryOutput = -1
					calls++
					if calls > 70 {
						return "", fmt.Errorf("call cap")
					}
					text, err := generateSummaryObserved(ctx, old, msgs, func(u *schema.TokenUsage) {
						if u != nil {
							summaryTokens = u.PromptTokens
							summaryOutput = u.CompletionTokens
						}
					})
					if err != nil {
						safe := strings.ReplaceAll(err.Error(), conf.QwenConfig.QwenApiKey, "[redacted]")
						t.Logf("summary failure: %s", safe)
					}
					return text, err
				}
				bounded, e := a.ContextMessages(context.Background())
				prep := time.Since(started).Milliseconds()
				if e != nil {
					t.Fatal(e)
				}
				recent := bounded
				if len(recent) > 0 && recent[0].Role == schema.Assistant {
					recent = recent[1:]
				}
				groups := [][]*schema.Message{utils.ConvertToSchemaMessages(all), recent, bounded}
				for step := 0; step < 3; step++ {
					g := (step + repeat + round) % 3
					if calls >= 70 {
						t.Fatal("call cap")
					}
					calls++
					m, e := NewQwenModel(context.Background())
					if e != nil {
						t.Fatal(e)
					}
					begin := time.Now()
					reply, e := m.llm.Generate(context.Background(), groups[g])
					row := map[string]any{"scenario": scenario, "repeat": repeat, "round": round, "group": g, "answer_ms": time.Since(begin).Milliseconds(), "success": e == nil, "prep_ms": int64(0), "summary_input": 0, "summary_output": 0}
					if g == 2 {
						row["prep_ms"] = prep
						row["summary_input"] = summaryTokens
						row["summary_output"] = summaryOutput
					}
					if e != nil {
						failures++
						consecutiveFailures++
						row["error"] = strings.ReplaceAll(e.Error(), conf.QwenConfig.QwenApiKey, "[redacted]")
					} else {
						consecutiveFailures = 0
						row["answer"] = reply.Content
						if reply.ResponseMeta != nil && reply.ResponseMeta.Usage != nil {
							row["input_tokens"] = reply.ResponseMeta.Usage.PromptTokens
							row["output_tokens"] = reply.ResponseMeta.Usage.CompletionTokens
						}
						expected := map[string]string{"person": fmt.Sprintf("人物%d", scenario), "onset": "2026-01-03", "allergy": "青霉素", "preference": "GREEN", "pending": "预约", "diagnosis": "unknown"}
						if round >= 4 {
							expected["preference"] = "YELLOW"
						}
						var actual map[string]any
						parseErr := json.Unmarshal([]byte(strings.TrimSpace(reply.Content)), &actual)
						scores := map[string]bool{}
						count := 0
						for k, v := range expected {
							scores[k] = parseErr == nil && actual[k] == v
							if scores[k] {
								count++
							}
						}
						row["field_scores"] = scores
						row["fields_correct"] = count
						row["correct"] = count == 6
					}
					row["summary_present"] = len(bounded) > 0 && bounded[0].Role == schema.Assistant
					row["history_characters"] = textSize(all)
					row["summary_version"] = a.summary.state.Version
					if err := enc.Encode(row); err != nil {
						t.Fatal(err)
					}
					t.Logf("scenario=%d repeat=%d round=%d group=%d", scenario, repeat, round, g)
					if consecutiveFailures >= 3 {
						t.Fatal("three consecutive answer failures")
					}
				}
				a.AppendMessage(&model.Message{UID: "synthetic", SessionID: sid, Content: "已记录。", Role: "assistant"}, false)
			}
		}
	}
	if failures > 0 {
		t.Fatalf("%d answer requests failed; results cannot establish compression benefit", failures)
	}
}
