package aihelper

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"GoAI/config"
	"GoAI/internal/testutil"
	"GoAI/model"
	"GoAI/utils"
	"github.com/cloudwego/eino/schema"
)

type informationDataset struct {
	Scenarios []struct {
		ID          string
		Messages    []struct{ ID, Role, Content string }
		Checkpoints []struct {
			Checkpoint, Through int
			Question            string
			Fields              map[string]json.RawMessage
		}
	}
}

func informationQuestion(question string, fields map[string]json.RawMessage) string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	format, _ := json.Marshal(keys)
	return question + "\n只输出JSON对象，字段为" + string(format) + "。日期可写2026年具体日期；数值保留单位；事项字段用字符串数组；未提供的信息填未提供。只根据历史作答，不补充诊疗建议。"
}
func informationBudget(data informationDataset) (int, []int) {
	estimates := []int{}
	budget := int(^uint(0) >> 1)
	for _, s := range data.Scenarios {
		cp := s.Checkpoints[0]
		messages := []*schema.Message{}
		for _, m := range s.Messages[:cp.Through] {
			messages = append(messages, &schema.Message{Content: m.Content})
		}
		messages = append(messages, schema.UserMessage(informationQuestion(cp.Question, cp.Fields)))
		estimate := estimateInputTokens(messages)
		estimates = append(estimates, estimate)
		if estimate*90/100 < budget {
			budget = estimate * 90 / 100
		}
	}
	return budget, estimates
}

// Independent opt-in experiment; preserves the older live comparison and its archives.
func TestSummaryInformationLive(t *testing.T) {
	if os.Getenv("SUMMARY_INFORMATION_LIVE") != "1" {
		t.Skip("set SUMMARY_INFORMATION_LIVE=1 and GOPHERAI_TEST_MYSQL=1")
	}
	db := testutil.MySQL(t)
	if err := db.AutoMigrate(&model.Session{}, &model.Message{}); err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("SUMMARY_RESULTS")
	if dir == "" {
		t.Fatal("SUMMARY_RESULTS must be a new directory")
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	datasetPath := "experiments/summary-information-v1/dataset.json"
	if os.Getenv("SUMMARY_LONG_LIVE") == "1" {
		datasetPath = "experiments/summary-information-long-v1/dataset.json"
	}
	raw, err := os.ReadFile(datasetPath)
	if err != nil {
		t.Fatal(err)
	}
	var data informationDataset
	if err = json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	mustWrite := func(name string, v any) {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0644); e != nil {
			t.Fatal(e)
		}
	}
	mustWrite("dataset.json", json.RawMessage(raw))
	frozen := map[string]string{}
	for _, path := range []string{datasetPath, "scripts/summary_information.py", "common/aihelper/summary_information_test.go", "common/aihelper/context.go", "common/aihelper/summary.go"} {
		source, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		frozen[filepath.Base(path)] = fmt.Sprintf("%x", sha256.Sum256(source))
	}
	mustWrite("freeze.json", frozen)
	budget, firstEstimates := informationBudget(data)
	budgetRule := "floor(0.9 * min(first checkpoint inputs)), frozen before calls"
	if os.Getenv("SUMMARY_LONG_LIVE") == "1" {
		budget = historyTokenBudget
		budgetRule = "production Helper history budget, frozen before calls"
	}
	conf := config.GetConfig()
	mustWrite("metadata.json", map[string]any{"history_budget_estimated_tokens": budget, "first_checkpoint_estimates": firstEstimates, "budget_rule": budgetRule, "model": conf.QwenConfig.QwenModelName, "thinking": conf.QwenConfig.EnableThinking, "summary_thinking": conf.QwenConfig.EnableThinking, "dataset_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "started_at": time.Now().UTC(), "summary_timeout_seconds": 30, "call_cap": 70, "configuration": "NewQwenModel for all calls, unchanged defaults"})
	f, err := os.Create(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	write := func(v any) {
		if e := enc.Encode(v); e != nil {
			t.Fatal(e)
		}
		if e := f.Sync(); e != nil {
			t.Fatal(e)
		}
	}
	calls, answers, consecutive := 0, 0, 0
	complete := false
	defer func() {
		mustWrite("status.json", map[string]any{"complete": complete, "calls": calls, "answers": answers})
	}()
	safe := func(e error) string {
		if e == nil {
			return ""
		}
		s := e.Error()
		if key := conf.QwenConfig.QwenApiKey; key != "" {
			s = strings.ReplaceAll(s, key, "[redacted]")
		}
		return s
	}
	for si, s := range data.Scenarios {
		sid := "information-" + s.ID
		a := NewAIHelper(nil, sid)
		a.summaryStore = mysqlSummaryRepository{db: db}
		if e := db.Create(&model.Session{ID: sid, UID: "synthetic"}).Error; e != nil {
			t.Fatal(e)
		}
		position := 0
		for _, cp := range s.Checkpoints {
			for position < cp.Through {
				x := s.Messages[position]
				msg := &model.Message{UID: "synthetic", SessionID: sid, StableID: x.ID, IsUser: x.Role == "user", Content: x.Content}
				a.AppendMessage(msg, false)
				if e := db.Create(msg).Error; e != nil {
					t.Fatal(e)
				}
				position++
			}
			old := a.summary.state
			a.summarize = func(ctx context.Context, previous string, msgs []*model.Message) (string, error) {
				if calls >= 70 {
					return "", fmt.Errorf("call cap")
				}
				calls++
				row := map[string]any{"kind": "summary", "scenario": s.ID, "checkpoint": cp.Checkpoint, "call": calls, "previous_summary": previous, "messages": msgs, "input_tokens": nil, "output_tokens": nil}
				input, _ := summaryModelInput(previous, msgs)
				if target, ok := ctx.Value(summaryTargetKey{}).(int); ok {
					input[0].Content += fmt.Sprintf("摘要含历史注入说明的估算token尽量不超过%d，使摘要与近期原文合计尽量低于预算80%%；这是生成目标，不是硬限制。", target)
				}
				row["input"] = input
				row["estimated_input_tokens"] = estimateInputTokens(input)
				begin := time.Now()
				txt, e := generateSummaryObserved(ctx, previous, msgs, func(u *schema.TokenUsage) {
					if u != nil {
						row["input_tokens"] = u.PromptTokens
						row["output_tokens"] = u.CompletionTokens
					}
				})
				row["text"] = txt
				row["error"] = safe(e)
				row["elapsed_ms"] = time.Since(begin).Milliseconds()
				row["timeout"] = ctx.Err() == context.DeadlineExceeded
				write(row)
				return txt, e
			}
			// Build against a fixed copy plus the current evaluation question.
			trial := NewAIHelper(nil, sid)
			trial.messages = a.GetMessages()
			trial.AppendMessage(&model.Message{UID: "synthetic", SessionID: sid, IsUser: true, Content: informationQuestion(cp.Question, cp.Fields)}, false)
			trial.summary = a.summary
			trial.summaryFailure = a.summaryFailure
			trial.summaryFailurePosition = a.summaryFailurePosition
			trial.summaryStore = a.summaryStore
			trial.summarize = a.summarize
			bounded, e := trial.contextMessagesWithBudget(context.Background(), budget, func(row map[string]any) {
				row["scenario"] = s.ID
				row["checkpoint"] = cp.Checkpoint
				write(row)
			})
			a.summary = trial.summary
			a.summaryFailure = trial.summaryFailure
			a.summaryFailurePosition = trial.summaryFailurePosition
			if e != nil {
				t.Fatal(e)
			}
			state := a.summary.state
			write(map[string]any{"kind": "snapshot", "scenario": s.ID, "checkpoint": cp.Checkpoint, "state": state, "previous_version": old.Version, "fallback": state.Version == old.Version && len(bounded) < len(trial.GetMessages()), "history_through": cp.Through})
			recent := bounded
			if len(recent) > 0 && recent[0].Role == schema.System {
				recent = recent[1:]
			}
			groups := [][]*schema.Message{utils.ConvertToSchemaMessages(trial.GetMessages()), recent, bounded}
			for step := 0; step < 3; step++ {
				if calls >= 70 {
					t.Fatal("call cap; incomplete")
				}
				g := (step + si + cp.Checkpoint - 1) % 3
				calls++
				answers++
				input := groups[g]
				row := map[string]any{"kind": "answer", "scenario": s.ID, "checkpoint": cp.Checkpoint, "group": string("ABC"[g]), "call": calls, "input": input, "estimated_input_tokens": estimateInputTokens(input), "summary_version": state.Version, "input_tokens": nil, "output_tokens": nil}
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				begin := time.Now()
				m, e := NewQwenModel(ctx)
				var reply *schema.Message
				if e == nil {
					reply, e = m.llm.Generate(ctx, input)
				}
				cancel()
				if e == nil && reply == nil {
					e = fmt.Errorf("empty answer")
				}
				row["error"] = safe(e)
				row["success"] = e == nil
				row["elapsed_ms"] = time.Since(begin).Milliseconds()
				if e != nil {
					consecutive++
				} else {
					consecutive = 0
					row["answer"] = reply.Content
					if reply.ResponseMeta != nil && reply.ResponseMeta.Usage != nil {
						row["input_tokens"] = reply.ResponseMeta.Usage.PromptTokens
						row["output_tokens"] = reply.ResponseMeta.Usage.CompletionTokens
					}
				}
				write(row)
				t.Logf("scenario=%s checkpoint=%d group=%s calls=%d success=%t", s.ID, cp.Checkpoint, row["group"], calls, e == nil)
				if consecutive >= 3 {
					t.Fatal("three consecutive answer failures; incomplete")
				}
			}
		}
	}
	complete = answers == 60
}
