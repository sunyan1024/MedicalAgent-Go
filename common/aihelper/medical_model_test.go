package aihelper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Assert the serialized provider requests, including both thinking fields emitted
// by the Qwen SDK. The server also rejects incompatible requests like DashScope.
func TestMedicalThinkingRequests(t *testing.T) {
	for _, thinking := range []bool{false, true} {
		for _, streaming := range []bool{false, true} {
			t.Run(fmt.Sprintf("thinking=%v/stream=%v", thinking, streaming), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req struct {
						Thinking bool `json:"enable_thinking"`
						Template struct {
							Thinking bool `json:"enable_thinking"`
						} `json:"chat_template_kwargs"`
						ToolChoice json.RawMessage `json:"tool_choice"`
						Stream     bool            `json:"stream"`
					}
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					var choice struct {
						Type     string `json:"type"`
						Function struct {
							Name string `json:"name"`
						} `json:"function"`
					}
					_ = json.Unmarshal(req.ToolChoice, &choice)
					forced := choice.Function.Name == "search_medical_knowledge"
					if wantForced := r.Header.Get("X-Test-Forced") == "true"; forced != wantForced || (forced && choice.Type != "function") {
						t.Errorf("tool_choice=%s, want forced search=%v", req.ToolChoice, wantForced)
					}
					wantThinking := thinking && !forced
					if req.Thinking != wantThinking || req.Template.Thinking != wantThinking || req.Stream != streaming {
						t.Errorf("thinking=%v template=%v stream=%v, want thinking=%v stream=%v, choice=%s", req.Thinking, req.Template.Thinking, req.Stream, wantThinking, streaming, req.ToolChoice)
					}
					if forced && req.Thinking {
						http.Error(w, "forced choice is incompatible with thinking", http.StatusBadRequest)
						return
					}
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\ndata: {\"id\":\"test\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
					} else {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"id":"test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)
					}
				}))
				defer server.Close()
				base, err := qwen.NewChatModel(context.Background(), &qwen.ChatModelConfig{
					BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "test-model", EnableThinking: &thinking,
				})
				if err != nil {
					t.Fatal(err)
				}
				m := &medicalChatModel{base}
				tool := &schema.ToolInfo{Name: "search_medical_knowledge", Desc: "search", ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
					"query": {Type: schema.String, Required: true},
				})}
				invoke := func(forced bool) error {
					opts := []model.Option{model.WithTools([]*schema.ToolInfo{tool}), qwen.WithExtraHeader(map[string]string{"X-Test-Forced": fmt.Sprint(forced)})}
					if forced {
						// Even a per-request true override must yield to the compatibility rule.
						opts = append(opts, model.WithToolChoice(schema.ToolChoiceForced, tool.Name), qwen.WithEnableThinking(true))
					}
					msgs := []*schema.Message{schema.UserMessage("test")}
					if !streaming {
						_, err := m.Generate(context.Background(), msgs, opts...)
						return err
					}
					s, err := m.Stream(context.Background(), msgs, opts...)
					if err != nil {
						return err
					}
					defer s.Close()
					for {
						_, err = s.Recv()
						if err == io.EOF {
							return nil
						}
						if err != nil {
							return err
						}
					}
				}
				// Initial search, autonomous continuation, redelegation, then summary.
				for _, forced := range []bool{true, false, true, false} {
					if err := invoke(forced); err != nil {
						t.Fatal(err)
					}
				}
				var wg sync.WaitGroup
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func(forced bool) {
						defer wg.Done()
						if err := invoke(forced); err != nil {
							t.Error(err)
						}
					}(i%2 == 0)
				}
				wg.Wait()
			})
		}
	}
}
