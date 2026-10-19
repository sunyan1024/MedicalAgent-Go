package aihelper

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"GoAI/common/medical"
	"GoAI/internal/testutil"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/schema"
)

// Explicitly opt-in: verifies provider-native ToolCall support with a tiny
// synthetic corpus, without accessing or altering application data.
func TestMedicalLiveSmoke(t *testing.T) {
	if os.Getenv("GOPHERAI_MEDICAL_LIVE") != "1" {
		t.Skip("set GOPHERAI_MEDICAL_LIVE=1 to call configured Qwen provider")
	}
	conf := testutil.Config(t)
	for _, thinking := range []bool{false, true} {
		t.Run(fmt.Sprintf("thinking=%v", thinking), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
			defer cancel()
			m, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{
				BaseURL: conf.QwenConfig.QwenBaseUrl, APIKey: conf.QwenConfig.QwenApiKey,
				Model: conf.QwenConfig.QwenModelName, EnableThinking: &thinking,
			})
			if err != nil {
				t.Fatal(err)
			}
			r := medical.NewRuntime("live-smoke", medical.Dependencies{Model: &medicalChatModel{m}, Search: func(context.Context, string) ([]medical.Source, error) {
				return []medical.Source{{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_1", Title: "合成软件测试资料", Content: "这是软件测试资料，没有医学事实。演示标识是 DEMO_OK。"}}, nil
			}})
			answer, err := r.Run(ctx, []*schema.Message{schema.UserMessage("请调用 medical_researcher 检索知识库中的合成测试资料，查询演示标识。必须先检索，依据工具返回的原文报告演示标识，不要自行推断或补充医学事实。")})
			if err != nil {
				for _, event := range r.Events {
					if event.Type == "tool_end" || event.Type == "subagent_end" {
						t.Logf("%s %s %s: %s", event.Agent, event.Tool, event.Status, event.Summary)
					}
				}
				t.Fatal(err)
			}
			called, completed, resolved := false, false, false
			for _, e := range r.Events {
				if e.Type == "subagent_end" && e.Tool == "medical_researcher" && e.Status == "completed" {
					completed = true
					resolved = strings.Contains(e.Output, "knowledge_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa_1")
				}
				if e.Type == "tool_start" && e.Tool == "search_medical_knowledge" {
					called = true
				}
			}
			if !called || !completed || !resolved || !strings.Contains(answer, "DEMO_OK") {
				t.Fatalf("provider did not complete evidence flow: called=%v completed=%v resolved=%v answer=%q", called, completed, resolved, answer)
			}
			t.Logf("native ToolCall and child agent completed; events=%d", len(r.Events))
		})
	}
}
