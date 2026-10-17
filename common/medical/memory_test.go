package medical

import (
	"context"
	"errors"
	"strings"
	"testing"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func TestMemoryToolsIsolationBudgetAndSafeEvents(t *testing.T) {
	logs := captureLogs(t)
	secret := "用户自述私密过敏史"
	doc := MemoryDocument{ContentMarkdown: secret, Version: 1}
	r := NewRuntime("memory", Dependencies{Memory: &MemoryAccess{
		Read: func(context.Context) (MemoryDocument, error) { return doc, nil },
		Update: func(_ context.Context, content string, version int64) (MemoryDocument, error) {
			if version != doc.Version {
				return MemoryDocument{}, errors.New("版本冲突")
			}
			doc.ContentMarkdown = content
			doc.Version++
			return doc, nil
		},
	}})
	local := 0
	ts := r.memoryTools(&local)
	read, update := ts[0].(tool.InvokableTool), ts[1].(tool.InvokableTool)
	out, err := read.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, secret) {
		t.Fatal(out, err)
	}
	out, err = update.InvokableRun(context.Background(), `{"content_markdown":"","expected_version":0}`)
	if err != nil || !strings.Contains(out, "版本冲突") {
		t.Fatal(out, err)
	}
	out, err = update.InvokableRun(context.Background(), `{"content_markdown":"","expected_version":1}`)
	if err != nil || doc.Version != 2 || doc.ContentMarkdown != "" {
		t.Fatal(out, err)
	}
	if r.used != 0 || local != 0 || r.memoryUsed != 3 {
		t.Fatal("memory consumed medical budget")
	}
	if _, err = read.InvokableRun(context.Background(), `{}`); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	for _, e := range r.Events {
		if strings.HasPrefix(e.Type, "subagent") || strings.Contains(JSON(e), secret) {
			t.Fatal(e)
		}
	}
	if strings.Contains(logs.String(), secret) {
		t.Fatal("memory leaked into logs")
	}
	if !strings.Contains(r.Events[1].Output, secret) {
		t.Fatal("raw event body lost")
	}
	// Exhausting medical calls still leaves the independent memory allowance.
	r.memoryUsed = 0
	r.used = 9
	local = 3
	if _, err = read.InvokableRun(context.Background(), `{}`); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryToolValidation(t *testing.T) {
	for _, raw := range []string{`{}`, `{"content_markdown":"x","expected_version":-1}`, `{"content_markdown":"x","expected_version":0,"uid":"other"}`, `{"content_markdown":null,"expected_version":0}`, `{"content_markdown":"x","expected_version":1.5}`, `{"content_markdown":"x","expected_version":0} {}`} {
		r := NewRuntime("invalid", Dependencies{Memory: &MemoryAccess{Update: func(context.Context, string, int64) (MemoryDocument, error) {
			t.Fatal("invalid update invoked store")
			return MemoryDocument{}, nil
		}}})
		local := 0
		out, err := r.memoryTools(&local)[1].(tool.InvokableTool).InvokableRun(context.Background(), raw)
		if err != nil || !strings.Contains(out, "error") {
			t.Fatal(out, err)
		}
	}
}

type memoryScenario struct {
	step      int
	sawMemory bool
}

func (m *memoryScenario) Generate(_ context.Context, msgs []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	if strings.Contains(msgs[0].Content, "医学证据检索子 Agent") {
		for _, msg := range msgs {
			if msg.Role == schema.Tool {
				return schema.AssistantMessage(supportedResult, nil), nil
			}
		}
		return evidenceCall("search_medical_knowledge", m.step), nil
	}
	for _, msg := range msgs {
		if strings.Contains(msg.Content, `"version":4`) && strings.Contains(msg.Content, "简洁中文") {
			m.sawMemory = true
		}
	}
	m.step++
	name, args := "medical_researcher", `{"request":"查找证据"}`
	if m.step%2 == 0 {
		name, args = "read_user_memory", `{}`
	}
	if m.step > 6 {
		return schema.AssistantMessage("已完成", nil), nil
	}
	return schema.AssistantMessage("", []schema.ToolCall{{ID: JSON(m.step), Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}}), nil
}
func (m *memoryScenario) Stream(ctx context.Context, msgs []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	v, err := m.Generate(ctx, msgs, opts...)
	return schema.StreamReaderFromArray([]*schema.Message{v}), err
}
func TestMemoryInjectionAndSixCallsThenAnswer(t *testing.T) {
	m := &memoryScenario{}
	r := NewRuntime("six", Dependencies{Model: m, Search: func(context.Context, string) ([]Source, error) {
		return []Source{{ID: "memory-test", Content: "测试证据"}}, nil
	}, Memory: &MemoryAccess{Read: func(context.Context) (MemoryDocument, error) {
		return MemoryDocument{ContentMarkdown: "简洁中文", Version: 4}, nil
	}}})
	answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("查询")})
	if err != nil || answer != "已完成" || !m.sawMemory || r.memoryUsed != 3 || r.used != 6 {
		t.Fatal(answer, err, m, r.used, r.memoryUsed)
	}
}

func TestMemoryBudgetFiltersExhaustedCategory(t *testing.T) {
	r := NewRuntime("filter", Dependencies{Memory: &MemoryAccess{}})
	local := 3
	m := &budgetModel{runtime: r, local: &local, main: true, tools: []*schema.ToolInfo{toolInfo("read_user_memory", "", ""), toolInfo("medical_researcher", "", "request")}}
	_, opts := m.prepare(nil, nil)
	available := einomodel.GetCommonOptions(nil, opts...).Tools
	if len(available) != 1 || available[0].Name != "read_user_memory" {
		t.Fatal(available)
	}
	r.memoryUsed = 3
	local = 0
	_, opts = m.prepare(nil, nil)
	available = einomodel.GetCommonOptions(nil, opts...).Tools
	if len(available) != 1 || available[0].Name != "medical_researcher" {
		t.Fatal(available)
	}
	local = 3
	_, opts = m.prepare(nil, nil)
	if *einomodel.GetCommonOptions(nil, opts...).ToolChoice != schema.ToolChoiceForbidden {
		t.Fatal("tools not disabled")
	}
}
