package medical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type scenarioModel struct {
	mu          sync.Mutex
	calls       map[string]int
	readSource  bool
	withReport  bool
	loop        bool
	childInput  []*schema.Message
	childOutput *string
}

func (m *scenarioModel) Generate(ctx context.Context, msgs []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls == nil {
		m.calls = map[string]int{}
	}
	role := "main"
	if strings.Contains(msgs[0].Content, "你是报告分析子 Agent") {
		role = "report"
	}
	if strings.Contains(msgs[0].Content, "你是医学证据检索子 Agent") {
		role = "research"
	}
	n := m.calls[role]
	m.calls[role]++
	call := func(name, args string) *schema.Message {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: fmt.Sprintf("%s_%d", role, n), Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}})
	}
	if role == "main" {
		if n == 0 || m.loop {
			if m.withReport && n == 0 {
				return call("report_analyst", `{"request":"解读已确认报告"}`), nil
			}
			return call("medical_researcher", `{"request":"查找指标解释证据"}`), nil
		}
		if m.withReport && n == 1 {
			return call("medical_researcher", `{"request":"查询报告指标相关资料"}`), nil
		}
		return schema.AssistantMessage("已完成，资料见 [knowledge_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_1]。", nil), nil
	}
	if role == "report" {
		if m.childOutput != nil {
			return schema.AssistantMessage(*m.childOutput, nil), nil
		}
		if n == 0 {
			return call("read_report", `{}`), nil
		}
		if n == 1 {
			return call("compare_reference_ranges", `{}`), nil
		}
		return schema.AssistantMessage(`{"findings":["指标偏高"],"evidence":["第1行"],"missing":[]}`, nil), nil
	}
	if n == 0 {
		m.childInput = append([]*schema.Message(nil), msgs...)
	}
	if m.childOutput != nil {
		return schema.AssistantMessage(*m.childOutput, nil), nil
	}
	if n%3 == 0 {
		return call("search_medical_knowledge", `{"query":"第一次查询"}`), nil
	}
	if n%3 == 1 && m.readSource {
		return call("read_medical_source", `{"sourceId":"S1"}`), nil
	}
	if n%3 == 1 {
		return call("search_medical_knowledge", `{"query":"改写后的查询"}`), nil
	}
	return schema.AssistantMessage(`{"findings":["检索到相关资料"],"evidence":["S1"],"missing":[]}`, nil), nil
}
func (m *scenarioModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	out, err := m.Generate(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{out}), nil
}

func TestAgentDelegationJSONFormats(t *testing.T) {
	valid := "{\"findings\":[\"保留 ``` 和 ' 正文\"],\"evidence\":[],\"missing\":[]}"
	fence := func(s string) string { return "```json\n" + s + "\n```" }
	for _, tc := range []struct {
		name, output, wantError string
	}{
		{name: "plain", output: valid},
		{name: "whitespace", output: " \n" + valid + "\t\n"},
		{name: "json fence", output: fence(valid)},
		{name: "bare fence", output: "```\n" + valid + "\n```"},
		{name: "fence whitespace CRLF", output: " \n```json\r\n" + valid + "\r\n```\t\n"},
		{name: "unclosed fence", output: "```json\n" + valid, wantError: "请按约定JSON格式返回"},
		{name: "prose before", output: "结果如下\n" + fence(valid), wantError: "请按约定JSON格式返回"},
		{name: "prose after", output: fence(valid) + "\n结束", wantError: "请按约定JSON格式返回"},
		{name: "invalid JSON", output: fence(`{"findings":`), wantError: "unexpected end of JSON input"},
		{name: "wrong type", output: fence(`{"findings":[{}],"evidence":[],"missing":[]}`), wantError: "cannot unmarshal object"},
		{name: "missing array", output: fence(`{"findings":[],"evidence":[]}`), wantError: "缺少 findings"},
		{name: "null array", output: fence(`{"findings":[],"evidence":[],"missing":null}`), wantError: "缺少 findings"},
		{name: "unknown source", output: fence(`{"findings":[],"evidence":["knowledge_unknown"],"missing":[]}`), wantError: "本次未检索到的来源"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRuntime("json-formats", Dependencies{Model: &scenarioModel{withReport: true, childOutput: &tc.output}})
			if _, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("解读报告并查询证据")}); err != nil {
				t.Fatal(err)
			}
			ends := 0
			for _, e := range r.Events {
				if e.Type != "subagent_end" {
					continue
				}
				ends++
				wantError := tc.wantError
				// Report evidence is a report location, not a retrieved source ID.
				if tc.name == "unknown source" && ends == 1 {
					wantError = ""
				}
				if wantError != "" {
					if e.Status != "failed" || !strings.Contains(e.Error, wantError) || e.Output != tc.output {
						t.Fatalf("unexpected failure event: %+v", e)
					}
					continue
				}
				if e.Status != "completed" || e.Error != "" || !json.Valid([]byte(e.Output)) {
					t.Fatalf("unexpected success event: %+v", e)
				}
				if tc.wantError == "" && e.Output != valid {
					t.Fatalf("JSON content changed: %s", e.Output)
				}
			}
			if ends != 2 || r.used != 2 {
				t.Fatalf("want exactly two delegations without retries: ends=%d used=%d", ends, r.used)
			}
		})
	}
}

func TestAgentDelegationRequeryAndContextIsolation(t *testing.T) {
	m := &scenarioModel{withReport: true}
	queries := []string{}
	r := NewRuntime("test", Dependencies{Model: m, ReadReport: func(context.Context) ([]Indicator, error) {
		return []Indicator{{Name: "合成指标", Value: "12", Unit: "U", Reference: "1-10", Source: "第1行"}}, nil
	}, Search: func(_ context.Context, q string) ([]Source, error) {
		queries = append(queries, q)
		if len(queries) == 1 {
			return nil, nil
		}
		return []Source{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_1", Title: "测试资料", Content: "测试证据"}}, nil
	}})
	answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("仅主Agent可见的历史"), schema.UserMessage("请解释报告")})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "已完成，资料见 。" {
		t.Fatalf("answer: %s", answer)
	}
	if len(queries) != 2 || queries[0] == queries[1] {
		t.Fatalf("not replanned: %v", queries)
	}
	if r.used != 6 {
		t.Fatalf("want 6 calls including delegation, got %d", r.used)
	}
	for _, msg := range m.childInput {
		if strings.Contains(msg.Content, "仅主Agent可见的历史") {
			t.Fatal("parent history leaked to child")
		}
	}
	starts := 0
	for _, e := range r.Events {
		if e.Type == "subagent_start" {
			starts++
		}
		if e.Type == "tool_start" && e.ParentID == "" {
			t.Fatal("missing delegation parent")
		}
	}
	if starts != 2 {
		t.Fatalf("delegations: %d", starts)
	}
}

func TestKnowledgeOnlyDoesNotReadReport(t *testing.T) {
	r := NewRuntime("test", Dependencies{Model: &scenarioModel{}, ReadReport: func(context.Context) ([]Indicator, error) { t.Fatal("knowledge question read report"); return nil, nil }, Search: func(context.Context, string) ([]Source, error) {
		return []Source{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_1"}}, nil
	}})
	if _, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("什么是血常规")}); err != nil {
		t.Fatal(err)
	}
}

func TestBoundedToolsBudgetErrorsAndCancellation(t *testing.T) {
	r := NewRuntime("test", Dependencies{})
	local := 0
	calls := 0
	task := r.bounded("child", "parent", &local, toolInfo("tool", "test", ""), func(context.Context, string, string) (string, error) { calls++; return "", errors.New("retryable") }).(tool.InvokableTool)
	for i := 0; i < 3; i++ {
		out, err := task.InvokableRun(context.Background(), `{}`)
		if err != nil || !strings.Contains(out, "retryable") {
			t.Fatalf("error not observation: %s %v", out, err)
		}
	}
	if _, err := task.InvokableRun(context.Background(), `{}`); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatal(calls)
	}
	local = 0
	r.used = 9
	if _, err := task.InvokableRun(context.Background(), `{}`); !errors.Is(err, ErrBudget) {
		t.Fatal("global budget not enforced")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.used = 0
	if _, err := task.InvokableRun(ctx, `{}`); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel ignored")
	}
}

func TestAgentLoopTerminates(t *testing.T) {
	r := NewRuntime("loop", Dependencies{Model: &scenarioModel{loop: true}, Search: func(context.Context, string) ([]Source, error) { return nil, nil }})
	answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("循环")})
	if err == nil || r.used > 9 || !strings.Contains(answer, "未完成") {
		t.Fatalf("used=%d err=%v answer=%s", r.used, err, answer)
	}
}

func TestCompareDoesNotInventRanges(t *testing.T) {
	items := []Indicator{{Value: "2", Unit: "U", Reference: "1-3"}, {Value: "4", Unit: "U", Reference: "1～3"}, {Value: "0", Unit: "U", Reference: "1-3"}, {Value: "4", Unit: "U"}, {Value: "NaN", Unit: "U", Reference: "1-3"}, {Value: "4", Unit: "mg/L", Reference: "1-3 mmol/L"}, {Value: "2", Reference: "1-3"}, {Value: "2", Unit: "U", Reference: "3-1"}}
	want := []string{"范围内", "偏高", "偏低", "待核对", "待核对", "待核对", "待核对", "待核对"}
	for i, f := range Compare(items) {
		if f.Status != want[i] {
			t.Fatalf("case %d: %s", i, f.Status)
		}
	}
}

func TestParameterValidation(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"query":1}`, `{"query":"ok","uid":"victim"}`} {
		if _, err := argument(raw, "query"); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestMCPTransportUsesRealToolResult(t *testing.T) {
	sources, err := searchMCP(context.Background(), "测试查询", func(_ context.Context, q string) ([]Source, error) {
		if q != "测试查询" {
			t.Fatal(q)
		}
		return []Source{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_1", Title: "测试", Content: "证据"}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Content != "证据" {
		t.Fatal(sources)
	}
}

func TestCancellationStopsInFlightChildTool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	r := NewRuntime("cancel", Dependencies{Model: &scenarioModel{}, Search: func(ctx context.Context, _ string) ([]Source, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	done := make(chan error, 1)
	go func() { _, err := r.Run(ctx, []*schema.Message{schema.UserMessage("查询资料")}); done <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("child tool did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation was not propagated")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not stop after cancellation")
	}
}

func TestBudgetModelDisablesToolsForFinalSummary(t *testing.T) {
	r := NewRuntime("budget", Dependencies{})
	local := 3
	m := &budgetModel{runtime: r, local: &local}
	input := []*schema.Message{schema.UserMessage("question")}
	messages, opts := m.prepare(input, []model.Option{model.WithTools([]*schema.ToolInfo{toolInfo("search", "test", "query")})})
	options := model.GetCommonOptions(nil, opts...)
	if len(options.Tools) != 0 || options.ToolChoice == nil || *options.ToolChoice != schema.ToolChoiceForbidden {
		t.Fatal("exhausted agent can still request tools")
	}
	if len(input) != 1 || !strings.Contains(messages[len(messages)-1].Content, "必须根据已有结果") {
		t.Fatal("final summary instruction missing or input mutated")
	}
	local = 0
	r.used = 9
	_, opts = m.prepare(input, nil)
	if model.GetCommonOptions(nil, opts...).ToolChoice == nil {
		t.Fatal("shared budget did not disable tools")
	}
}

func TestKnowledgeSourcesConsistentAcrossToolResultsAndEvents(t *testing.T) {
	rawID := strings.Repeat("a", 64) + "_1"
	for _, id := range []string{rawID, "knowledge_" + rawID} {
		t.Run(id, func(t *testing.T) {
			expected := Source{ID: "knowledge_" + rawID, Kind: "knowledge", Title: "资料", Content: "证据"}
			r := NewRuntime("sources", Dependencies{Model: &scenarioModel{readSource: true}, Search: func(context.Context, string) ([]Source, error) {
				return []Source{{ID: id, Title: expected.Title, Content: expected.Content}}, nil
			}})
			answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("读取检索资料")})
			if err != nil || answer != "已完成，资料见 。" {
				t.Fatalf("answer=%s err=%v", answer, err)
			}
			if len(r.sources) != 1 || r.sources[expected.ID] != expected {
				t.Fatalf("cache: %+v", r.sources)
			}
			modelSource := expected
			modelSource.ID = "S1"
			sourceEvents, searchResults, readResults := 0, 0, 0
			for _, e := range r.Events {
				if e.Type == "subagent_end" && e.Tool == "medical_researcher" {
					var result struct {
						Evidence []string `json:"evidence"`
					}
					if err := json.Unmarshal([]byte(e.Output), &result); err != nil || e.Status != "completed" || len(result.Evidence) != 1 || result.Evidence[0] != expected.ID {
						t.Fatalf("delegation did not restore full source ID: %+v", e)
					}
				}
				if e.Type == "sources" {
					sourceEvents++
					if len(e.Sources) != 1 || e.Sources[0] != expected {
						t.Fatalf("sources: %+v", e.Sources)
					}
				}
				if e.Type != "tool_end" {
					continue
				}
				if e.Status != "completed" {
					t.Fatalf("tool failed: %+v", e)
				}
				switch e.Tool {
				case "search_medical_knowledge":
					searchResults++
					var got []Source
					if err := json.Unmarshal([]byte(e.Output), &got); err != nil || len(got) != 1 || got[0] != modelSource {
						t.Fatalf("search output: %s (%v)", e.Output, err)
					}
				case "read_medical_source":
					readResults++
					var got Source
					if err := json.Unmarshal([]byte(e.Output), &got); err != nil || got != modelSource {
						t.Fatalf("read output: %s (%v)", e.Output, err)
					}
				}
				if e.Summary != e.Output {
					t.Fatalf("short output and summary differ: %+v", e)
				}
			}
			if sourceEvents != 1 || searchResults != 1 || readResults != 1 {
				t.Fatalf("events=%d search=%d read=%d", sourceEvents, searchResults, readResults)
			}
		})
	}
}
