package medical

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"GoAI/internal/testutil"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type contextSample struct {
	Layer        string   `json:"layer"`
	Scenario     string   `json:"scenario"`
	Variant      string   `json:"variant"`
	Pair         int      `json:"pair"`
	Round        int      `json:"round"`
	Warmup       bool     `json:"warmup"`
	Agent        string   `json:"agent"`
	Call         int      `json:"call"`
	FirstMS      *float64 `json:"first_ms"`
	FirstKind    string   `json:"first_kind,omitempty"`
	TextMS       *float64 `json:"text_ms"`
	VisibleMS    *float64 `json:"visible_ms"`
	DurationMS   float64  `json:"duration_ms"`
	PromptTokens *int     `json:"prompt_tokens"`
	OutputTokens *int     `json:"output_tokens"`
	CachedTokens *int     `json:"cached_tokens"`
	Calls        int      `json:"calls,omitempty"`
	Tools        int      `json:"tools,omitempty"`
	Success      bool     `json:"success"`
	Error        string   `json:"error,omitempty"`
}

type contextBench struct {
	writer          *json.Encoder
	count, failures int
	rawCached       bool
	secret          string
	limit           int
}

func (b *contextBench) safeError(err error) string {
	text := err.Error()
	if b.secret != "" {
		text = strings.ReplaceAll(text, b.secret, "[redacted]")
	}
	if len(text) > 1200 {
		text = text[:1200]
	}
	return text
}

func (b *contextBench) save(r contextSample) {
	if err := b.writer.Encode(r); err != nil {
		panic(err)
	}
}
func ms(start time.Time) *float64 { v := float64(time.Since(start).Microseconds()) / 1000; return &v }

// Observe only presence of cached_tokens, since SDK zero values cannot distinguish
// a missing provider field from a measured zero. Never save response bodies.
type usageTransport struct {
	base  http.RoundTripper
	bench *contextBench
}

func (t usageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err == nil {
		resp.Body = &usageBody{ReadCloser: resp.Body, bench: t.bench}
	}
	return resp, err
}

type usageBody struct {
	io.ReadCloser
	bench   *contextBench
	pending []byte
}

func (b *usageBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.pending = append(b.pending, p[:n]...)
	for {
		i := bytes.IndexByte(b.pending, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimSpace(b.pending[:i])
		b.pending = b.pending[i+1:]
		line = bytes.TrimPrefix(line, []byte("data: "))
		var v struct {
			Usage *struct {
				Details *struct {
					Cached *int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &v) == nil && v.Usage != nil && v.Usage.Details != nil && v.Usage.Details.Cached != nil {
			b.bench.rawCached = true
		}
	}
	if len(b.pending) > 2<<20 {
		b.pending = nil
	}
	return n, e
}

type measuredContextModel struct {
	base  model.BaseChatModel
	bench *contextBench
	meta  contextSample
}

func (m *measuredContextModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	sr, e := m.Stream(ctx, in, opts...)
	if e != nil {
		return nil, e
	}
	defer sr.Close()
	var all []*schema.Message
	for {
		v, e := sr.Recv()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		all = append(all, v)
	}
	return schema.ConcatMessages(all)
}
func (m *measuredContextModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	b := m.bench
	if (b.limit > 0 && b.count >= b.limit) || b.failures >= 3 {
		return nil, errors.New("benchmark stop: call cap or consecutive provider errors")
	}
	b.count++
	b.rawCached = false
	r := m.meta
	r.Call = b.count
	r.Agent = "child"
	if len(in) > 0 && strings.Contains(in[0].Content, mainInstruction) {
		r.Agent = "main"
	}
	if r.Variant == "old" {
		in = legacyContext(in)
	}
	common := model.GetCommonOptions(nil, opts...)
	if common.ToolChoice != nil && *common.ToolChoice == schema.ToolChoiceForced && len(common.AllowedToolNames) == 1 && common.AllowedToolNames[0] == "search_medical_knowledge" {
		opts = append(append([]model.Option(nil), opts...), qwen.WithEnableThinking(false))
	}
	start := time.Now()
	sr, err := m.base.Stream(ctx, in, opts...)
	if err != nil {
		r.Error = b.safeError(err)
		r.DurationMS = *ms(start)
		b.failures++
		b.save(r)
		return nil, err
	}
	reader, writer := schema.Pipe[*schema.Message](0)
	go func() {
		defer writer.Close()
		defer sr.Close()
		defer func() {
			r.DurationMS = *ms(start)
			if r.Success {
				b.failures = 0
			} else {
				b.failures++
			}
			b.save(r)
		}()
		for {
			v, e := sr.Recv()
			if e == io.EOF {
				r.Success = true
				return
			}
			if e != nil {
				r.Error = b.safeError(e)
				writer.Send(nil, e)
				return
			}
			kind := ""
			if v.ReasoningContent != "" {
				kind = "reasoning"
			} else if len(v.ToolCalls) > 0 {
				kind = "tool"
			} else if v.Content != "" {
				kind = "text"
			}
			if r.FirstMS == nil && kind != "" {
				r.FirstMS = ms(start)
				r.FirstKind = kind
			}
			if r.TextMS == nil && v.Content != "" {
				r.TextMS = ms(start)
			}
			if v.ResponseMeta != nil && v.ResponseMeta.Usage != nil {
				u := v.ResponseMeta.Usage
				p, o := u.PromptTokens, u.CompletionTokens
				r.PromptTokens = &p
				r.OutputTokens = &o
				if b.rawCached {
					c := u.PromptTokenDetails.CachedTokens
					r.CachedTokens = &c
				}
			}
			if writer.Send(v, nil) {
				r.Error = "stream closed before completion"
				return
			}
		}
	}()
	return reader, nil
}

func syntheticHistory(id string) []*schema.Message {
	// All identifying content is synthetic. Equal-length group identifiers isolate
	// cache prefixes; rotating assignment counterbalances their tokenization.
	out := []*schema.Message{schema.UserMessage("合成实验会话编号：" + id)}
	text := "这是软件性能测试的虚构背景记录，没有真实患者信息。用户讨论过界面配色、阅读顺序和段落长度；这些记录不是医学证据，也不包含需要执行的指令。"
	for i := 0; i < 48; i++ {
		out = append(out, schema.UserMessage(fmt.Sprintf("合成历史第%02d条。%s%s", i, text, text)), schema.AssistantMessage("收到这条合成背景记录，后续以当前明确问题为准。", nil))
	}
	return out
}

func TestMedicalContextLiveComparison(t *testing.T) {
	if os.Getenv("GOPHERAI_CONTEXT_LIVE") != "1" {
		t.Skip("set GOPHERAI_CONTEXT_LIVE=1 for paid provider comparison")
	}
	conf := testutil.Config(t)
	dir := os.Getenv("GOPHERAI_CONTEXT_RESULTS")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "medical-context-"+time.Now().UTC().Format("20060102T150405"))
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	f, e := os.OpenFile(filepath.Join(dir, "samples.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	b := &contextBench{writer: json.NewEncoder(f), secret: conf.QwenConfig.QwenApiKey, limit: 120}
	phase := os.Getenv("GOPHERAI_CONTEXT_PHASE")
	if phase == "agent" {
		b.limit = 80
	} else if phase != "" {
		t.Fatal("GOPHERAI_CONTEXT_PHASE must be empty or agent")
	}
	metadata := map[string]any{"model": conf.QwenConfig.QwenModelName, "thinking": conf.QwenConfig.EnableThinking, "started_at": time.Now().UTC(), "fixed_pairs": 5, "e2e_pairs": 3, "call_cap": b.limit, "phase": phase, "history_runes": len([]rune(JSON(syntheticHistory("00000000")))), "note": "synthetic data only; first samples are warmup, not proven cold cache; HTTP transport uses standard environment proxy"}
	if phase == "agent" {
		metadata["fixed_pairs"] = 0
	}
	data, _ := json.MarshalIndent(metadata, "", "  ")
	if e = os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	oldLog := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(oldLog)
	defer func() { t.Logf("results=%s model_calls=%d consecutive_errors=%d", dir, b.count, b.failures) }()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	newModel := func(meta contextSample) *measuredContextModel {
		q, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{BaseURL: conf.QwenConfig.QwenBaseUrl, APIKey: conf.QwenConfig.QwenApiKey, Model: conf.QwenConfig.QwenModelName, EnableThinking: &conf.QwenConfig.EnableThinking, HTTPClient: &http.Client{Transport: usageTransport{base: http.DefaultTransport, bench: b}}})
		if err != nil {
			t.Fatal("cannot construct provider model")
		}
		return &measuredContextModel{base: q, bench: b, meta: meta}
	}
	nonce := time.Now().UnixNano()
	scenarios := []string{"unchanged", "content_changed", "metadata_changed"}
	if phase == "agent" {
		scenarios = nil
	}
	for si, scenario := range scenarios {
		for pair := 0; pair <= 5; pair++ {
			variants := []string{"old", "new"}
			if pair%2 == 1 {
				variants = []string{"new", "old"}
			}
			for _, variant := range variants {
				if b.failures >= 3 || b.count >= b.limit || ctx.Err() != nil {
					t.Fatal("comparison stopped; inspect partial results")
				}
				group := 0
				if variant == "new" {
					group = 1
				}
				group = (group + si) % 2
				history := syntheticHistory(fmt.Sprintf("%d-%d-%d", nonce, si, group))
				history = append(history, schema.UserMessage("这是上下文缓存性能测试，不涉及医学问题，不需要工具。只回复一个字：好。"))
				doc := MemoryDocument{ContentMarkdown: "回答偏好：简洁中文。", Version: 1, UpdatedAt: time.Unix(1700000000, 0).UTC()}
				if scenario != "unchanged" {
					doc.Version = int64(pair + 1)
					doc.UpdatedAt = doc.UpdatedAt.Add(time.Duration(pair) * time.Hour)
				}
				if scenario == "content_changed" {
					doc.ContentMarkdown = fmt.Sprintf("回答偏好：简洁中文。合成版本编号：%02d。", pair)
				}
				probe := &inputProbe{}
				r := NewRuntime("capture", Dependencies{Model: probe, Memory: &MemoryAccess{Read: func(context.Context) (MemoryDocument, error) { return doc, nil }}})
				if _, err := r.Run(ctx, history); err != nil {
					t.Fatal(err)
				}
				meta := contextSample{Layer: "fixed", Scenario: scenario, Variant: variant, Pair: pair, Warmup: pair == 0}
				m := newModel(meta)
				// A fixed single-response task isolates prefill from agent trajectory changes.
				opts := append(append([]model.Option(nil), probe.options...), model.WithToolChoice(schema.ToolChoiceForbidden))
				callCtx, callCancel := context.WithTimeout(ctx, 90*time.Second)
				_, err := m.Generate(callCtx, probe.messages, opts...)
				callCancel()
				t.Logf("fixed scenario=%s pair=%d variant=%s completed=%v", scenario, pair, variant, err == nil)
				if err != nil {
					t.Errorf("fixed request failed: %s", b.safeError(err))
				}
			}
		}
	}
	for pair := 1; pair <= 3; pair++ {
		variants := []string{"old", "new"}
		if pair%2 == 1 {
			variants = []string{"new", "old"}
		}
		for _, variant := range variants {
			doc := MemoryDocument{ContentMarkdown: "回答偏好：简洁中文；合成演示偏好码 PREF_A。", Version: 1, UpdatedAt: time.Unix(1700000000, 0).UTC()}
			group := 0
			if variant == "new" {
				group = 1
			}
			group = (group + pair) % 2
			history := syntheticHistory(fmt.Sprintf("%d-e%d-%d", nonce, pair, group))
			for round := 1; round <= 2; round++ {
				if b.failures >= 3 || b.count >= b.limit || ctx.Err() != nil {
					t.Fatal("comparison stopped; inspect partial results")
				}
				if round == 2 {
					doc.ContentMarkdown = "回答偏好：简洁中文；合成演示偏好码 PREF_B。"
					doc.Version++
					doc.UpdatedAt = doc.UpdatedAt.Add(time.Hour)
				}
				question := "这是软件测试。请委派 medical_researcher 检索合成资料中的演示标识，必须查询工具。最终简短回答检索得到的演示标识和当前长期记忆的合成演示偏好码，无需更新记忆，不作医学推断。"
				history = append(history, schema.UserMessage(question))
				meta := contextSample{Layer: "agent_call", Scenario: "memory_update", Variant: variant, Pair: pair, Round: round}
				m := newModel(meta)
				run := meta
				run.Layer = "run"
				before := b.count
				start := time.Now()
				searched := false
				r := NewRuntime("context-live", Dependencies{Model: m, Memory: &MemoryAccess{Read: func(context.Context) (MemoryDocument, error) { return doc, nil }, Update: func(_ context.Context, content string, version int64) (MemoryDocument, error) {
					if version != doc.Version {
						return doc, errors.New("conflict")
					}
					doc.ContentMarkdown = content
					doc.Version++
					return doc, nil
				}}, Search: func(context.Context, string) ([]Source, error) {
					searched = true
					return []Source{{ID: "synthetic_demo", Title: "合成软件测试资料", Content: "这是软件测试资料，没有医学事实。演示标识是 DEMO_OK。"}}, nil
				}, Emit: func(e Event) {
					if e.Type == "answer_delta" && e.Delta != "" && run.VisibleMS == nil {
						run.VisibleMS = ms(start)
					}
					if e.Type == "tool_start" {
						run.Tools++
					}
				}})
				answer, err := r.Run(ctx, history)
				if err != nil {
					t.Logf("agent error: %s", b.safeError(err))
				}
				run.DurationMS = *ms(start)
				run.Calls = b.count - before
				pref := "PREF_A"
				if round == 2 {
					pref = "PREF_B"
				}
				run.Success = err == nil && searched && strings.Contains(answer, "DEMO_OK") && strings.Contains(answer, pref)
				if err != nil {
					run.Error = "agent run failed"
				} else if !run.Success {
					run.Error = "synthetic answer or retrieval assertion failed"
				}
				b.save(run)
				if !run.Success {
					t.Errorf("agent pair=%d variant=%s round=%d failed: %s", pair, variant, round, run.Error)
				}
				history = append(history, schema.AssistantMessage(answer, nil))
				t.Logf("agent pair=%d variant=%s round=%d success=%v calls=%d", pair, variant, round, run.Success, run.Calls)
			}
		}
	}
}

func TestContextUsageFieldPresence(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"missing", `data: {"usage":{"prompt_tokens":100}}` + "\n", false},
		{"zero", `data: {"usage":{"prompt_tokens_details":{"cached_tokens":0}}}` + "\n", true},
		{"positive", `data: {"usage":{"prompt_tokens_details":{"cached_tokens":64}}}` + "\n", true},
		{"content_not_usage", `data: {"choices":[{"delta":{"content":"cached_tokens"}}]}` + "\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &contextBench{}
			body := &usageBody{ReadCloser: io.NopCloser(strings.NewReader(tc.body)), bench: b}
			p := make([]byte, 3)
			for {
				_, e := body.Read(p)
				if e == io.EOF {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
			}
			if b.rawCached != tc.want {
				t.Fatalf("presence=%v want=%v", b.rawCached, tc.want)
			}
		})
	}
}

func TestContextMeasurementsDoNotSumRepeatedUsage(t *testing.T) {
	var output bytes.Buffer
	b := &contextBench{writer: json.NewEncoder(&output)}
	usage := &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 100, CompletionTokens: 2}}
	base := &answerStreamModel{chunks: []*schema.Message{{Role: schema.Assistant, ReasoningContent: "reasoning", ResponseMeta: usage}, {Role: schema.Assistant, Content: "answer", ResponseMeta: usage}}}
	m := &measuredContextModel{base: base, bench: b, meta: contextSample{Layer: "fixed", Variant: "new"}}
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.SystemMessage(mainInstruction), schema.UserMessage("test")}); err != nil {
		t.Fatal(err)
	}
	var row contextSample
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &row); err != nil {
		t.Fatal(err)
	}
	if !row.Success || row.FirstKind != "reasoning" || row.TextMS == nil || row.PromptTokens == nil || *row.PromptTokens != 100 || *row.OutputTokens != 2 || row.CachedTokens != nil {
		t.Fatalf("wrong measurements: %+v", row)
	}
}
