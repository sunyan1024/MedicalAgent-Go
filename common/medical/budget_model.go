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
}

func (m *budgetModel) prepare(msgs []*schema.Message, opts []model.Option) ([]*schema.Message, []model.Option) {
	m.runtime.mu.Lock()
	remaining := min(3-*m.local, 9-m.runtime.used)
	m.runtime.mu.Unlock()
	out := append([]*schema.Message(nil), msgs...)
	instruction := fmt.Sprintf("本Agent当前剩余工具预算：%d 次。已经得到充分结果时立即总结；不要重复调用相同工具和参数。每次模型回复最多调用一个工具。", remaining)
	if remaining <= 0 {
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
