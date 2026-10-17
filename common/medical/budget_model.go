package medical

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// budgetModel supplies live limits on each generation, and disables tools when
// the next model response must summarize. The tool-side checks remain authoritative.
type budgetModel struct {
	base    model.BaseChatModel
	runtime *Runtime
	local   *int
	main    bool
	tools   []*schema.ToolInfo
	refs    *sourceReferences
}

func (m *budgetModel) prepare(msgs []*schema.Message, opts []model.Option) ([]*schema.Message, []model.Option) {
	m.runtime.mu.Lock()
	remaining := max(0, min(3-*m.local, 9-m.runtime.used))
	memoryRemaining := 0
	if m.main && m.runtime.deps.Memory != nil {
		memoryRemaining = max(0, 3-m.runtime.memoryUsed)
	}
	m.runtime.mu.Unlock()
	out := append([]*schema.Message(nil), msgs...)
	instruction := fmt.Sprintf("本Agent当前剩余工具预算：%d 次。已经得到充分结果时立即总结；不要重复调用相同工具和参数。每次模型回复最多调用一个工具。", remaining+memoryRemaining)
	if m.refs != nil && m.refs.needsSearch() && remaining > 0 {
		opts = append(append([]model.Option(nil), opts...), model.WithToolChoice(schema.ToolChoiceForced, "search_medical_knowledge"))
		instruction += " 本次委派尚未检索，必须先调用 search_medical_knowledge；首次检索后自主决定是否继续检索。"
	}
	if m.main && m.runtime.deps.Memory != nil {
		var available []*schema.ToolInfo
		for _, info := range m.tools {
			if (isMemoryTool(info.Name) && memoryRemaining > 0) || (!isMemoryTool(info.Name) && remaining > 0) {
				available = append(available, info)
			}
		}
		opts = append(append([]model.Option(nil), opts...), model.WithTools(available))
		instruction += fmt.Sprintf(" 医学委派可用 %d 次，记忆工具另有 %d 次；任一类别用完不得再调用该类别。", remaining, memoryRemaining)
	}
	if remaining+memoryRemaining <= 0 {
		instruction = "工具预算已用完，本轮必须根据已有结果输出最终回答；不再调用工具。证据不足就明确列出缺失信息。"
		opts = append(append([]model.Option(nil), opts...), model.WithTools(nil), model.WithToolChoice(schema.ToolChoiceForbidden))
	}
	out = append(out, schema.SystemMessage(instruction))
	return out, opts
}
func (m *budgetModel) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	msgs, opts = m.prepare(msgs, opts)
	return m.base.Generate(ctx, msgs, opts...)
}
func (m *budgetModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msgs, opts = m.prepare(msgs, opts)
	return m.base.Stream(ctx, msgs, opts...)
}
