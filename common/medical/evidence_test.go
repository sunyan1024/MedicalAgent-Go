package medical

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Scripts deliberately may ignore tool choice to exercise backend enforcement.
type evidenceModel struct {
	main, research   []*schema.Message
	mainN, researchN int
	choices          []*model.Options
}

func (m *evidenceModel) Generate(_ context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	if strings.Contains(msgs[0].Content, "你是医学证据检索子 Agent") {
		m.choices = append(m.choices, model.GetCommonOptions(nil, opts...))
		if m.researchN >= len(m.research) {
			return nil, fmt.Errorf("unexpected research generation")
		}
		out := m.research[m.researchN]
		m.researchN++
		return out, nil
	}
	if m.mainN >= len(m.main) {
		return nil, fmt.Errorf("unexpected main generation")
	}
	out := m.main[m.mainN]
	m.mainN++
	return out, nil
}
func (m *evidenceModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	out, err := m.Generate(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{out}), nil
}
func evidenceCall(name string, n int) *schema.Message {
	args := `{"query":"医学证据"}`
	if name == "medical_researcher" {
		args = `{"request":"查找医学证据"}`
	}
	return schema.AssistantMessage("", []schema.ToolCall{{ID: fmt.Sprintf("%s-%d", name, n), Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}})
}
func evidenceJSON(raw string) *schema.Message { return schema.AssistantMessage(raw, nil) }

const supportedResult = `{"findings":["有支持的结论"],"evidence":["S1"],"missing":[]}`
const emptyResult = `{"findings":[],"evidence":[],"missing":[]}`

func TestResearchEvidenceEnforcement(t *testing.T) {
	for _, tc := range []struct {
		name        string
		research    []*schema.Message
		searchErr   error
		sources     []Source
		wantError   string
		wantSuccess bool
	}{
		{name: "skip with forged alias", research: []*schema.Message{evidenceJSON(supportedResult)}, wantError: "未执行检索"},
		{name: "skip with empty evidence", research: []*schema.Message{evidenceJSON(`{"findings":["无依据结论"],"evidence":[],"missing":[]}`)}, wantError: "未执行检索"},
		{name: "skip with empty result", research: []*schema.Message{evidenceJSON(emptyResult)}, wantError: "未执行检索"},
		{name: "empty search", research: []*schema.Message{evidenceCall("search_medical_knowledge", 0), evidenceJSON(emptyResult)}},
		{name: "service failure", research: []*schema.Message{evidenceCall("search_medical_knowledge", 0), evidenceJSON(emptyResult)}, searchErr: errors.New("服务失败")},
		{name: "cached sources are insufficient", research: []*schema.Message{evidenceCall("search_medical_knowledge", 0), evidenceJSON(`{"findings":["无引用结论"],"evidence":[],"missing":[]}`)}, sources: []Source{{ID: "test", Content: "证据"}}, wantError: "结论缺少有效检索证据"},
		{name: "unknown alias", research: []*schema.Message{evidenceCall("search_medical_knowledge", 0), evidenceJSON(`{"findings":["结论"],"evidence":["S2"],"missing":[]}`)}, sources: []Source{{ID: "test", Content: "证据"}}, wantError: "本次未检索到的来源"},
		{name: "full ID rejected", research: []*schema.Message{evidenceCall("search_medical_knowledge", 0), evidenceJSON(`{"findings":["结论"],"evidence":["knowledge_test"],"missing":[]}`)}, sources: []Source{{ID: "test", Content: "证据"}}, wantError: "本次未检索到的来源"},
		{name: "valid", research: []*schema.Message{evidenceCall("search_medical_knowledge", 0), evidenceJSON(supportedResult)}, sources: []Source{{ID: "test", Content: "证据"}}, wantSuccess: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &evidenceModel{main: []*schema.Message{evidenceCall("medical_researcher", 0), schema.AssistantMessage("模型最终结论", nil)}, research: tc.research}
			var deltas string
			r := NewRuntime(tc.name, Dependencies{Model: m, Search: func(context.Context, string) ([]Source, error) { return tc.sources, tc.searchErr }, Emit: func(e Event) {
				if e.Type == "answer_delta" {
					deltas += e.Delta
				}
			}})
			answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("提问")})
			if tc.wantSuccess {
				if err != nil || answer != "模型最终结论" || deltas != answer {
					t.Fatalf("answer=%q deltas=%q err=%v", answer, deltas, err)
				}
				assertFinalAnswer(t, r.Events, answer, "completed")
			} else {
				if !errors.Is(err, ErrNoEvidence) || !strings.Contains(answer, "证据不足") || strings.Contains(answer, "模型最终结论") || deltas != "" {
					t.Fatalf("answer=%q deltas=%q err=%v", answer, deltas, err)
				}
				assertFinalAnswer(t, r.Events, answer, "partial")
			}
			first := m.choices[0]
			if first.ToolChoice == nil || *first.ToolChoice != schema.ToolChoiceForced || len(first.AllowedToolNames) != 1 || first.AllowedToolNames[0] != "search_medical_knowledge" {
				t.Fatalf("first choice: %+v", first)
			}
			if len(m.choices) > 1 && m.choices[1].ToolChoice != nil && *m.choices[1].ToolChoice == schema.ToolChoiceForced {
				t.Fatal("second generation still forced")
			}
			for _, e := range r.Events {
				if e.Type != "subagent_end" {
					continue
				}
				if tc.wantError != "" && (e.Status != "failed" || !strings.Contains(e.Error, tc.wantError)) {
					t.Fatalf("unexpected validation event: %+v", e)
				}
				if tc.wantError == "" && !tc.wantSuccess && (!strings.Contains(e.Output, "检索") || e.Status != "completed") {
					t.Fatalf("missing empty-result reason: %+v", e)
				}
			}
		})
	}
}

func TestResearchRetryAndRedelegation(t *testing.T) {
	for _, redelegate := range []bool{false, true} {
		t.Run(fmt.Sprintf("redelegate=%v", redelegate), func(t *testing.T) {
			m := &evidenceModel{main: []*schema.Message{evidenceCall("medical_researcher", 0)}}
			m.research = []*schema.Message{evidenceCall("search_medical_knowledge", 0)}
			if redelegate {
				m.research = append(m.research, evidenceJSON(emptyResult))
				m.main = append(m.main, evidenceCall("medical_researcher", 1))
			}
			m.research = append(m.research, evidenceCall("search_medical_knowledge", 1), evidenceJSON(supportedResult))
			m.main = append(m.main, schema.AssistantMessage("有证据回答", nil))
			searches := 0
			r := NewRuntime("retry", Dependencies{Model: m, Search: func(context.Context, string) ([]Source, error) {
				searches++
				if searches == 1 {
					return nil, errors.New("首次失败")
				}
				return []Source{{ID: "retry", Content: "证据"}}, nil
			}})
			answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("提问")})
			if err != nil || answer != "有证据回答" || searches != 2 {
				t.Fatalf("answer=%q searches=%d err=%v", answer, searches, err)
			}
			index := 1
			if redelegate {
				index = 2 // Skip the first delegation's empty-result summary.
			}
			second := m.choices[index]
			forced := second.ToolChoice != nil && *second.ToolChoice == schema.ToolChoiceForced
			if forced != redelegate {
				t.Fatalf("second choice: %+v", second)
			}
			assertFinalAnswer(t, r.Events, answer, "completed")
		})
	}
}

func TestResearchForcedChoiceRespectsExhaustedBudget(t *testing.T) {
	r := NewRuntime("exhausted", Dependencies{})
	local := 0
	m := &budgetModel{runtime: r, local: &local, refs: newSourceReferences()}
	r.used = 9
	_, opts := m.prepare(nil, nil)
	choice := model.GetCommonOptions(nil, opts...)
	if choice.ToolChoice == nil || *choice.ToolChoice != schema.ToolChoiceForbidden || len(choice.Tools) != 0 {
		t.Fatalf("choice=%+v", choice)
	}
}

func TestNoEvidenceFallbackPreservesReportFindings(t *testing.T) {
	r := NewRuntime("report", Dependencies{})
	r.findings = Compare([]Indicator{{Name: "测试指标", Value: "12", Unit: "U", Reference: "1-10"}})
	answer := r.partial(ErrNoEvidence)
	if !strings.Contains(answer, "测试指标") || !strings.Contains(answer, "偏高") || !strings.Contains(answer, "证据不足") {
		t.Fatal(answer)
	}
}
