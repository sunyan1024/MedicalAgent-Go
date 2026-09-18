package medical

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestTavilyRequestAndSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("invalid request")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["query"] != "测试" || body["search_depth"] != "basic" || body["max_results"] != float64(5) || body["auto_parameters"] != false || body["include_answer"] != false || body["include_raw_content"] != false {
			t.Errorf("unexpected options: %v", body)
		}
		fmt.Fprint(w, `{"results":[{"title":"证据","url":"https://example.org/a#x","content":"片段"},{"url":"https://example.org/a#y","content":"重复"},{"url":"javascript:alert(1)","content":"无效"},{"url":"https://example.org/empty"},{"url":"https://example.org/b","content":"另一片段"}]}`)
	}))
	defer server.Close()
	sources, err := searchTavily(context.Background(), server.Client(), server.URL, "test-key", "测试")
	if err != nil || len(sources) != 2 {
		t.Fatal(sources, err)
	}
	if sources[0].Kind != "web" || sources[0].URL != "https://example.org/a" || !strings.HasPrefix(sources[0].ID, "web_") || sources[1].Title != "example.org" {
		t.Fatal(sources)
	}
	// The same public JSON shape is stored in the event sources column.
	var restored []Source
	if err = json.Unmarshal([]byte(JSON(sources)), &restored); err != nil || restored[0] != sources[0] {
		t.Fatal(restored, err)
	}

}

func TestTavilyFailures(t *testing.T) {
	for _, status := range []int{401, 403, 429, 432, 433, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprint(w, "secret upstream body")
			}))
			defer server.Close()
			_, err := searchTavily(context.Background(), server.Client(), server.URL, "key", "test")
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatal(err)
			}
		})
	}
	for _, body := range []string{`{"results":[]}`, `invalid`, strings.Repeat("x", (2<<20)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		result, err := searchTavily(context.Background(), server.Client(), server.URL, "key", "test")
		server.Close()
		if body == `{"results":[]}` {
			if err != nil || len(result) != 0 {
				t.Fatal(result, err)
			}
		} else if err == nil {
			t.Fatal("invalid response accepted")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(50 * time.Millisecond):
		}
	}))
	defer server.Close()
	client := server.Client()
	client.Timeout = 10 * time.Millisecond
	if _, err := searchTavily(context.Background(), client, server.URL, "key", "test"); err == nil {
		t.Fatal("missing timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := searchTavily(ctx, client, server.URL, "key", "test"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type webScenario struct {
	calls    map[string]int
	fail     bool
	disabled bool
	loop     bool
}

func (m *webScenario) Generate(_ context.Context, msgs []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	role := "main"
	if strings.Contains(msgs[0].Content, "你是医学证据检索子 Agent") {
		role = "child"
	}
	if m.calls == nil {
		m.calls = map[string]int{}
	}
	n := m.calls[role]
	m.calls[role]++
	call := func(name, args string) *schema.Message {
		return schema.AssistantMessage("", []schema.ToolCall{{ID: fmt.Sprintf("%s-%d", role, n), Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}})
	}
	if role == "main" {
		if n == 0 {
			return call("medical_researcher", `{"request":"请联网查询最新医学资料"}`), nil
		}
		return schema.AssistantMessage("已检索 [web_"+strings.Repeat("a", 64)+"] [web_invalid]", nil), nil
	}
	if m.disabled {
		if !strings.Contains(msgs[0].Content, "当前联网搜索不可用") {
			return nil, fmt.Errorf("missing unavailable instruction")
		}
		return schema.AssistantMessage(`{"findings":[],"evidence":[],"missing":["联网不可用"]}`, nil), nil
	}
	if n == 0 || m.loop {
		return call("search_web", `{"query":"医学资料"}`), nil
	}
	if m.fail && n == 1 {
		return call("search_medical_knowledge", `{"query":"本地医学资料"}`), nil
	}
	if m.fail {
		return schema.AssistantMessage(`{"findings":[],"evidence":[],"missing":["联网失败"]}`, nil), nil
	}
	return schema.AssistantMessage(`{"findings":["证据"],"evidence":["web_`+strings.Repeat("a", 64)+`"],"missing":[]}`, nil), nil
}
func (m *webScenario) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	v, e := m.Generate(ctx, msgs, opts...)
	if e != nil {
		return nil, e
	}
	return schema.StreamReaderFromArray([]*schema.Message{v}), nil
}
func TestWebAgentScenarios(t *testing.T) {
	for _, mode := range []string{"success", "failure", "disabled", "budget"} {
		t.Run(mode, func(t *testing.T) {
			webCalls, localCalls := 0, 0
			deps := Dependencies{Model: &webScenario{fail: mode == "failure", disabled: mode == "disabled", loop: mode == "budget"}, Search: func(context.Context, string) ([]Source, error) { localCalls++; return []Source{}, nil }}
			if mode != "disabled" {
				deps.WebSearch = func(context.Context, string) ([]Source, error) {
					webCalls++
					if mode == "failure" {
						return nil, fmt.Errorf("联网失败")
					}
					return []Source{{ID: "web_" + strings.Repeat("a", 64), Kind: "web", URL: "https://example.org", Content: "证据"}}, nil
				}
			}
			r := NewRuntime(mode, deps)
			answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("联网查询")})
			if mode != "budget" && err != nil {
				t.Fatal(err)
			}
			if mode == "success" && (webCalls != 1 || strings.Contains(answer, "web_") || strings.TrimSpace(answer) != "已检索") {
				t.Fatal(answer, webCalls)
			}
			if mode == "success" {
				var delta string
				var sources []Source
				for _, event := range r.Events {
					if event.Type == "answer_delta" {
						delta += event.Delta
					}
					sources = append(sources, event.Sources...)
				}
				if delta != answer || len(sources) != 1 || sources[0].ID != "web_"+strings.Repeat("a", 64) || sources[0].URL != "https://example.org" || sources[0].Content != "证据" {
					t.Fatalf("answer/trace mismatch: %q %q %+v", answer, delta, sources)
				}
			}
			if mode == "failure" && (webCalls != 1 || localCalls != 1) {
				t.Fatal(webCalls, localCalls)
			}
			if mode == "disabled" && webCalls != 0 {
				t.Fatal(webCalls)
			}
			if mode == "budget" && (webCalls > 3 || r.used > 9) {
				t.Fatal(webCalls, r.used)
			}
		})
	}
}
