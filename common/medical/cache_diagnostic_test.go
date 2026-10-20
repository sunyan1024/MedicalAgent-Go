package medical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"GoAI/internal/testutil"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Test-only wire capture. All fixtures are synthetic; request headers and output
// text are never persisted. Each HTTP attempt has its own usage and request file.
type cacheDiagnosticAttempt struct {
	Number              int                        `json:"number"`
	Host                string                     `json:"host"`
	Path                string                     `json:"path"`
	Status              int                        `json:"status"`
	RequestFile         string                     `json:"request_file"`
	RequestHash         string                     `json:"request_hash"`
	MessagesHash        string                     `json:"messages_hash"`
	ToolsHash           string                     `json:"tools_hash"`
	SystemHash          string                     `json:"first_system_hash"`
	Roles               []string                   `json:"roles"`
	ToolNames           []string                   `json:"tool_names"`
	Settings            map[string]json.RawMessage `json:"settings"`
	SameMessages        int                        `json:"same_leading_messages"`
	CommonChars         int                        `json:"common_content_chars"`
	FirstDifference     *int                       `json:"first_different_message"`
	PreviousRequestHash string                     `json:"previous_request_hash,omitempty"`
	HeaderRequestID     string                     `json:"header_request_id,omitempty"`
	ResponseID          string                     `json:"response_id,omitempty"`
	ResponseModel       string                     `json:"response_model,omitempty"`
	RawUsage            []json.RawMessage          `json:"raw_usage"`
	Error               string                     `json:"error,omitempty"`
}
type cacheDiagnosticRecord struct {
	Scenario     string                    `json:"scenario"`
	Variant      string                    `json:"variant"`
	Step         int                       `json:"step"`
	StartedAt    time.Time                 `json:"started_at"`
	DurationMS   float64                   `json:"duration_ms"`
	FirstMS      *float64                  `json:"first_ms"`
	SDKUsage     *schema.TokenUsage        `json:"sdk_usage"`
	RawCached    *int                      `json:"raw_cached_tokens"`
	UsageMatch   string                    `json:"usage_match"`
	FinishReason string                    `json:"finish_reason"`
	Success      bool                      `json:"success"`
	Error        string                    `json:"error,omitempty"`
	Attempts     []*cacheDiagnosticAttempt `json:"attempts"`
}
type cacheDiagnosticBudget struct {
	mu    sync.Mutex
	count int
}

func (b *cacheDiagnosticBudget) take() (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count >= 40 {
		return 0, errors.New("diagnostic HTTP limit reached (40)")
	}
	b.count++
	return b.count, nil
}

type cacheDiagnosticCapture struct {
	mu     sync.Mutex
	record *cacheDiagnosticRecord
}
type cacheDiagnosticTransport struct {
	base      http.RoundTripper
	budget    *cacheDiagnosticBudget
	capture   *cacheDiagnosticCapture
	directory string
	previous  []byte
}

func cacheDiagnosticHash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func cacheDiagnosticCanonical(b []byte) []byte {
	var v any
	if len(b) == 0 {
		return []byte("null")
	}
	if json.Unmarshal(b, &v) != nil {
		return b
	}
	out, _ := json.Marshal(v)
	return out
}
func cacheDiagnosticContent(m map[string]json.RawMessage) string {
	var s string
	if json.Unmarshal(m["content"], &s) == nil {
		return s
	}
	return string(cacheDiagnosticCanonical(m["content"]))
}

// Message/content prefix measures are not the provider's hidden chat-template
// token prefix. Compare every message field (including role and tool calls).
func cacheDiagnosticDescribe(body, previous []byte, a *cacheDiagnosticAttempt) error {
	var req map[string]json.RawMessage
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	a.RequestHash = cacheDiagnosticHash(body)
	a.MessagesHash = cacheDiagnosticHash(cacheDiagnosticCanonical(req["messages"]))
	a.ToolsHash = cacheDiagnosticHash(cacheDiagnosticCanonical(req["tools"]))
	a.Settings = map[string]json.RawMessage{}
	for _, key := range []string{"model", "tool_choice", "enable_thinking", "chat_template_kwargs", "max_tokens", "temperature", "top_p", "seed", "stream", "stream_options"} {
		if v, ok := req[key]; ok {
			a.Settings[key] = v
		}
	}
	var messages []map[string]json.RawMessage
	if err := json.Unmarshal(req["messages"], &messages); err != nil {
		return err
	}
	for i, m := range messages {
		var role string
		json.Unmarshal(m["role"], &role)
		a.Roles = append(a.Roles, role)
		if i == 0 && role == "system" {
			a.SystemHash = cacheDiagnosticHash(cacheDiagnosticCanonical(m["content"]))
		}
	}
	var tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	json.Unmarshal(req["tools"], &tools)
	for _, t := range tools {
		a.ToolNames = append(a.ToolNames, t.Function.Name)
	}
	if len(previous) == 0 {
		return nil
	}
	a.PreviousRequestHash = cacheDiagnosticHash(previous)
	var prev map[string]json.RawMessage
	if err := json.Unmarshal(previous, &prev); err != nil {
		return err
	}
	var before []map[string]json.RawMessage
	if err := json.Unmarshal(prev["messages"], &before); err != nil {
		return err
	}
	n := min(len(messages), len(before))
	for i := 0; i < n; i++ {
		if reflect.DeepEqual(messages[i], before[i]) {
			a.SameMessages++
			a.CommonChars += len([]rune(cacheDiagnosticContent(messages[i])))
			continue
		}
		index := i
		a.FirstDifference = &index
		if bytes.Equal(messages[i]["role"], before[i]["role"]) {
			x, y := []rune(cacheDiagnosticContent(messages[i])), []rune(cacheDiagnosticContent(before[i]))
			for j := 0; j < min(len(x), len(y)) && x[j] == y[j]; j++ {
				a.CommonChars++
			}
		}
		return nil
	}
	if len(messages) != len(before) {
		index := n
		a.FirstDifference = &index
	}
	return nil
}
func (t cacheDiagnosticTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n, err := t.budget.take()
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	a := &cacheDiagnosticAttempt{Number: n, Host: req.URL.Hostname(), Path: req.URL.Path, RequestFile: fmt.Sprintf("requests/%03d.json", n)}
	if err = cacheDiagnosticDescribe(body, t.previous, a); err != nil {
		return nil, err
	}
	// This transport is only constructed by the explicit synthetic live test.
	if err = os.WriteFile(filepath.Join(t.directory, a.RequestFile), body, 0600); err != nil {
		return nil, err
	}
	t.capture.mu.Lock()
	t.capture.record.Attempts = append(t.capture.record.Attempts, a)
	t.capture.mu.Unlock()
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	t.capture.mu.Lock()
	a.Status = resp.StatusCode
	a.HeaderRequestID = resp.Header.Get("X-Request-Id")
	t.capture.mu.Unlock()
	resp.Body = &cacheDiagnosticBody{ReadCloser: resp.Body, capture: t.capture, attempt: a}
	return resp, nil
}

type cacheDiagnosticBody struct {
	io.ReadCloser
	capture *cacheDiagnosticCapture
	attempt *cacheDiagnosticAttempt
	pending []byte
}

func (b *cacheDiagnosticBody) line(line []byte) {
	line = bytes.TrimSpace(line)
	if bytes.HasPrefix(line, []byte("data:")) {
		line = bytes.TrimSpace(line[5:])
	}
	var frame struct {
		ID    string          `json:"id"`
		Model string          `json:"model"`
		Usage json.RawMessage `json:"usage"`
	}
	if json.Unmarshal(line, &frame) != nil {
		return
	}
	b.capture.mu.Lock()
	defer b.capture.mu.Unlock()
	if frame.ID != "" {
		b.attempt.ResponseID = frame.ID
	}
	if frame.Model != "" {
		b.attempt.ResponseModel = frame.Model
	}
	if len(frame.Usage) > 0 && !bytes.Equal(frame.Usage, []byte("null")) {
		b.attempt.RawUsage = append(b.attempt.RawUsage, append(json.RawMessage(nil), frame.Usage...))
	}
}
func (b *cacheDiagnosticBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.pending = append(b.pending, p[:n]...)
	for {
		i := bytes.IndexByte(b.pending, '\n')
		if i < 0 {
			break
		}
		b.line(b.pending[:i])
		b.pending = b.pending[i+1:]
	}
	if err == io.EOF && len(b.pending) > 0 {
		b.line(b.pending)
		b.pending = nil
	}
	if len(b.pending) > 2<<20 {
		return n, errors.New("diagnostic SSE line exceeds 2 MiB")
	}
	return n, err
}
func cacheDiagnosticCompare(r *cacheDiagnosticRecord) {
	r.UsageMatch = "unavailable"
	if len(r.Attempts) == 0 {
		return
	}
	a := r.Attempts[len(r.Attempts)-1]
	if len(a.RawUsage) == 0 {
		return
	}
	var u struct {
		Prompt            *int `json:"prompt_tokens"`
		Completion        *int `json:"completion_tokens"`
		Total             *int `json:"total_tokens"`
		CompletionDetails *struct {
			Reasoning *int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
		Details *struct {
			Cached *int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if json.Unmarshal(a.RawUsage[len(a.RawUsage)-1], &u) != nil {
		return
	}
	if u.Details != nil {
		r.RawCached = u.Details.Cached
	}
	if r.SDKUsage == nil {
		return
	}
	r.UsageMatch = "match"
	for _, p := range []struct {
		raw *int
		sdk int
	}{{u.Prompt, r.SDKUsage.PromptTokens}, {u.Completion, r.SDKUsage.CompletionTokens}, {u.Total, r.SDKUsage.TotalTokens}, {r.RawCached, r.SDKUsage.PromptTokenDetails.CachedTokens}} {
		if p.raw != nil && *p.raw != p.sdk {
			r.UsageMatch = "mismatch"
		}
	}
	if u.CompletionDetails != nil && u.CompletionDetails.Reasoning != nil && *u.CompletionDetails.Reasoning != r.SDKUsage.CompletionTokensDetails.ReasoningTokens {
		r.UsageMatch = "mismatch"
	}
	if u.Prompt == nil || u.Completion == nil || u.Total == nil || r.RawCached == nil {
		if r.UsageMatch == "match" {
			r.UsageMatch = "partial"
		}
	}
}

func cacheDiagnosticInput(t *testing.T, history []*schema.Message, doc MemoryDocument) ([]*schema.Message, []model.Option) {
	t.Helper()
	p := &inputProbe{}
	r := NewRuntime("diagnostic-capture", Dependencies{Model: p, Memory: &MemoryAccess{Read: func(context.Context) (MemoryDocument, error) { return doc, nil }}})
	if _, err := r.Run(context.Background(), history); err != nil {
		t.Fatal(err)
	}
	return p.messages, p.options
}

func TestMedicalCacheDiagnosticLive(t *testing.T) {
	if os.Getenv("GOPHERAI_CACHE_DIAGNOSTIC_LIVE") != "1" {
		t.Skip("set GOPHERAI_CACHE_DIAGNOSTIC_LIVE=1 for up to 40 paid synthetic requests")
	}
	conf := testutil.Config(t)
	directory := os.Getenv("GOPHERAI_CACHE_DIAGNOSTIC_RESULTS")
	if directory == "" {
		directory = filepath.Join(os.TempDir(), "medical-cache-diagnostic-"+time.Now().UTC().Format("20060102T150405"))
	}
	if err := os.MkdirAll(filepath.Join(directory, "requests"), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(directory, "samples.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	encoder := json.NewEncoder(f)
	const maxOutput = 128
	endpoint, _ := url.Parse(conf.QwenConfig.QwenBaseUrl)
	metadata := map[string]any{"model": conf.QwenConfig.QwenModelName, "thinking": conf.QwenConfig.EnableThinking, "started_at": time.Now().UTC(), "planned_requests": 36, "http_limit": 40, "max_output_tokens": maxOutput, "provider_host": endpoint.Hostname(), "note": "Synthetic cache diagnosis only. Length-truncated output is not a semantic failure. Prefix character counts do not measure provider template tokens."}
	meta, _ := json.MarshalIndent(metadata, "", "  ")
	if err = os.WriteFile(filepath.Join(directory, "metadata.json"), meta, 0600); err != nil {
		t.Fatal(err)
	}
	before := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(before)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	budget := &cacheDiagnosticBudget{}
	last := map[string][]byte{}
	failures := 0
	nonce := time.Now().UnixNano()
	records := 0
	defer func() { t.Logf("results=%s logical_requests=%d http_attempts=%d", directory, records, budget.count) }()
	safeError := func(e error) string {
		s := strings.ReplaceAll(e.Error(), conf.QwenConfig.QwenApiKey, "[redacted]")
		if len(s) > 1500 {
			s = s[:1500]
		}
		return s
	}
	run := func(scenario, variant string, step int, messages []*schema.Message, opts []model.Option) *cacheDiagnosticRecord {
		if failures >= 3 || ctx.Err() != nil {
			t.Fatal("diagnostic stopped: consecutive errors or deadline")
		}
		r := &cacheDiagnosticRecord{Scenario: scenario, Variant: variant, Step: step, StartedAt: time.Now().UTC()}
		capture := &cacheDiagnosticCapture{record: r}
		key := scenario + "/" + variant
		transport := cacheDiagnosticTransport{base: http.DefaultTransport, budget: budget, capture: capture, directory: directory, previous: last[key]}
		maxTokens := maxOutput
		q, err := qwen.NewChatModel(ctx, &qwen.ChatModelConfig{BaseURL: conf.QwenConfig.QwenBaseUrl, APIKey: conf.QwenConfig.QwenApiKey, Model: conf.QwenConfig.QwenModelName, EnableThinking: &conf.QwenConfig.EnableThinking, MaxTokens: &maxTokens, HTTPClient: &http.Client{Transport: transport}})
		if err != nil {
			t.Fatal("cannot construct model")
		}
		callCtx, callCancel := context.WithTimeout(ctx, 60*time.Second)
		defer callCancel()
		start := time.Now()
		sr, err := q.Stream(callCtx, messages, opts...)
		if err == nil {
			for {
				chunk, e := sr.Recv()
				if e == io.EOF {
					r.Success = true
					break
				}
				if e != nil {
					err = e
					break
				}
				if r.FirstMS == nil && (chunk.Content != "" || chunk.ReasoningContent != "" || len(chunk.ToolCalls) > 0) {
					r.FirstMS = ms(start)
				}
				if chunk.ResponseMeta != nil {
					if chunk.ResponseMeta.Usage != nil {
						copy := *chunk.ResponseMeta.Usage
						r.SDKUsage = &copy
					}
					if chunk.ResponseMeta.FinishReason != "" {
						r.FinishReason = chunk.ResponseMeta.FinishReason
					}
				}
			}
			sr.Close()
		}
		r.DurationMS = *ms(start)
		if err != nil {
			r.Error = safeError(err)
			failures++
			t.Errorf("%s/%s/%d request failed: %s", scenario, variant, step, r.Error)
		} else {
			failures = 0
		}
		capture.mu.Lock()
		cacheDiagnosticCompare(r)
		if len(r.Attempts) > 0 {
			body, e := os.ReadFile(filepath.Join(directory, r.Attempts[len(r.Attempts)-1].RequestFile))
			if e != nil {
				t.Fatal(e)
			}
			last[key] = body
		}
		if e := encoder.Encode(r); e != nil {
			t.Fatal(e)
		}
		capture.mu.Unlock()
		records++
		if r.Success && r.UsageMatch != "match" {
			t.Errorf("%s/%s/%d usage comparison=%s", scenario, variant, step, r.UsageMatch)
		}
		t.Logf("%s/%s/%d success=%v cache=%v usage=%s", scenario, variant, step, r.Success, func() any {
			if r.RawCached == nil {
				return nil
			}
			return *r.RawCached
		}(), r.UsageMatch)
		return r
	}
	doc := func(version int64) MemoryDocument {
		return MemoryDocument{ContentMarkdown: fmt.Sprintf("回答偏好：简洁中文。合成记忆编号：%02d。", version), Version: version, UpdatedAt: time.Unix(1700000000+version*3600, 0).UTC()}
	}
	question := schema.UserMessage("这是软件缓存测试，不涉及医学。只回复：好。无需调用工具。")
	history := func(group string) []*schema.Message {
		return append(syntheticHistory(fmt.Sprintf("%d-%s", nonce, group)), question)
	}
	// Verify a genuinely long stable system prefix using provider input usage.
	var padding strings.Builder
	for i := 0; i < 640; i++ {
		fmt.Fprintf(&padding, "合成稳定前缀编号 %04d：只用于缓存诊断，不包含真实用户信息。\n", i)
	}
	pad := padding.String()
	calibration := run("calibration", "padding", 0, []*schema.Message{schema.SystemMessage(pad), schema.UserMessage("只回复：好。")}, nil)
	if !calibration.Success || calibration.SDKUsage == nil || calibration.SDKUsage.PromptTokens < 2048 {
		t.Fatal("padding input count unavailable or below 2048; cannot validate long-prefix diagnostic")
	}
	// Identical controls and changing-memory groups, alternating variant order.
	for step := 0; step < 3; step++ {
		variants := []string{"old", "new"}
		if step%2 == 1 {
			variants = []string{"new", "old"}
		}
		for _, v := range variants {
			m, o := cacheDiagnosticInput(t, history("repeat-"+v), doc(1))
			if v == "old" {
				m = legacyContext(m)
			}
			run("repeat", v, step, m, append(o, model.WithToolChoice(schema.ToolChoiceForbidden)))
		}
	}
	for step := 0; step < 3; step++ {
		variants := []string{"old", "new", "old_padded"}
		if step%2 == 1 {
			variants = []string{"old_padded", "new", "old"}
		}
		for _, v := range variants {
			m, o := cacheDiagnosticInput(t, history("change-"+v), doc(int64(step+1)))
			if v != "new" {
				m = legacyContext(m)
			}
			if v == "old_padded" {
				first := *m[0]
				first.Content = pad + first.Content
				m[0] = &first
			}
			run("changing_memory", v, step, m, append(o, model.WithToolChoice(schema.ToolChoiceForbidden)))
		}
	}
	// Six turns per variant: four stable-memory turns, one update, one stable turn.
	histories := map[string][]*schema.Message{"old": syntheticHistory(fmt.Sprintf("%d-sequence-old", nonce)), "new": syntheticHistory(fmt.Sprintf("%d-sequence-new", nonce))}
	for step := 0; step < 6; step++ {
		variants := []string{"old", "new"}
		if step%2 == 1 {
			variants = []string{"new", "old"}
		}
		version := int64(1)
		if step >= 4 {
			version = 2
		}
		for _, v := range variants {
			h := append(histories[v], schema.UserMessage(fmt.Sprintf("第%d轮合成缓存测试。只回复好，不涉及医学，不调用工具。", step+1)))
			m, o := cacheDiagnosticInput(t, h, doc(version))
			if v == "old" {
				m = legacyContext(m)
			}
			run("sequence", v, step, m, append(o, model.WithToolChoice(schema.ToolChoiceForbidden)))
			histories[v] = append(h, schema.AssistantMessage("合成历史回答：收到。", nil))
		}
	}
	// Isolate tool schema changes: same messages, same choice=none, only tools vary.
	fixedMessages, fixedOpts := cacheDiagnosticInput(t, history("tools"), doc(1))
	full := model.GetCommonOptions(nil, fixedOpts...).Tools
	for step := 0; step < 4; step++ {
		for _, v := range []string{"fixed", "filtered"} {
			tools := full
			if v == "filtered" && step >= 2 {
				r := NewRuntime("budget-probe", Dependencies{Memory: &MemoryAccess{}})
				local := 3
				if step == 3 {
					r.memoryUsed = 3
				}
				bm := &budgetModel{runtime: r, local: &local, main: true, tools: full}
				_, o := bm.prepare(nil, nil)
				tools = model.GetCommonOptions(nil, o...).Tools
			}
			run("tools", v, step, fixedMessages, []model.Option{model.WithTools(tools), model.WithToolChoice(schema.ToolChoiceForbidden)})
		}
	}
	if records != 36 {
		t.Errorf("expected 36 logical requests, got %d", records)
	}
}

func TestCacheDiagnosticWireUsageAndPrefix(t *testing.T) {
	previous := []byte(`{"messages":[{"role":"system","content":"固定规则"},{"role":"user","content":"旧问题"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"model":"synthetic","tool_choice":"none"}`)
	current := []byte(`{"model":"synthetic","messages":[{"role":"system","content":"固定规则"},{"role":"user","content":"新问题"}],"tools":[{"type":"function","function":{"name":"lookup"}}],"tool_choice":"none"}`)
	a := &cacheDiagnosticAttempt{}
	if err := cacheDiagnosticDescribe(current, previous, a); err != nil {
		t.Fatal(err)
	}
	if a.SameMessages != 1 || a.CommonChars != 4 || a.FirstDifference == nil || *a.FirstDifference != 1 || len(a.ToolNames) != 1 || a.ToolNames[0] != "lookup" {
		t.Fatalf("bad prefix %+v", a)
	}
	for _, tc := range []struct {
		name, usage string
		cached      *int
		match       string
	}{
		{"zero", `{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102,"prompt_tokens_details":{"cached_tokens":0}}`, func() *int { v := 0; return &v }(), "match"},
		{"missing", `{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102}`, nil, "partial"},
		{"positive", `{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102,"prompt_tokens_details":{"cached_tokens":64}}`, func() *int { v := 64; return &v }(), "match"},
		{"sdk_mismatch", `{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102,"prompt_tokens_details":{"cached_tokens":64}}`, func() *int { v := 0; return &v }(), "mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &cacheDiagnosticRecord{SDKUsage: &schema.TokenUsage{PromptTokens: 100, CompletionTokens: 2, TotalTokens: 102}, Attempts: []*cacheDiagnosticAttempt{{}}}
			if tc.cached != nil {
				r.SDKUsage.PromptTokenDetails.CachedTokens = *tc.cached
			}
			cap := &cacheDiagnosticCapture{record: r}
			wire := `data: {"id":"provider-test","model":"resolved-model","usage":null}` + "\n\n" + `data: {"usage":` + tc.usage + `}`
			body := &cacheDiagnosticBody{ReadCloser: io.NopCloser(strings.NewReader(wire)), capture: cap, attempt: r.Attempts[0]}
			var got bytes.Buffer
			p := make([]byte, 3)
			for {
				n, e := body.Read(p)
				got.Write(p[:n])
				if e == io.EOF {
					break
				}
				if e != nil {
					t.Fatal(e)
				}
			}
			if got.String() != wire {
				t.Fatal("wire capture altered provider response")
			}
			cacheDiagnosticCompare(r)
			if r.UsageMatch != tc.match || len(r.Attempts[0].RawUsage) != 1 || r.Attempts[0].ResponseID != "provider-test" || r.Attempts[0].ResponseModel != "resolved-model" {
				t.Fatalf("incorrect usage capture %+v", r)
			}
			if tc.name == "missing" && r.RawCached != nil {
				t.Fatal("missing cached_tokens must remain unknown")
			}
		})
	}
}

type cacheDiagnosticFakeTransport struct{ calls int }

func (f *cacheDiagnosticFakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls++
	data, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if !bytes.Contains(data, []byte("synthetic")) {
		return nil, errors.New("request changed")
	}
	return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{"request-test"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n"))}, nil
}
func TestCacheDiagnosticTransportBoundaryAndCap(t *testing.T) {
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "requests"), 0700); err != nil {
		t.Fatal(err)
	}
	fake := &cacheDiagnosticFakeTransport{}
	budget := &cacheDiagnosticBudget{count: 39}
	r := &cacheDiagnosticRecord{}
	transport := cacheDiagnosticTransport{base: fake, budget: budget, capture: &cacheDiagnosticCapture{record: r}, directory: directory}
	req, _ := http.NewRequest("POST", "https://synthetic.invalid/v1/chat/completions", strings.NewReader(`{"model":"synthetic","messages":[{"role":"user","content":"fixture"}]}`))
	req.Header.Set("Authorization", "Bearer synthetic-secret-must-not-be-saved")
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if fake.calls != 1 || len(r.Attempts) != 1 || r.Attempts[0].Number != 40 || r.Attempts[0].HeaderRequestID != "request-test" {
		t.Fatal("boundary not captured")
	}
	data, err := os.ReadFile(filepath.Join(directory, r.Attempts[0].RequestFile))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(r)
	if bytes.Contains(data, []byte("synthetic-secret")) || bytes.Contains(encoded, []byte("synthetic-secret")) {
		t.Fatal("authorization header persisted")
	}
	if _, err := transport.RoundTrip(req); err == nil || fake.calls != 1 {
		t.Fatal("HTTP cap did not stop request")
	}
}

func TestCacheDiagnosticReasoningUsageMismatch(t *testing.T) {
	r := &cacheDiagnosticRecord{SDKUsage: &schema.TokenUsage{PromptTokens: 100, CompletionTokens: 2, TotalTokens: 102}, Attempts: []*cacheDiagnosticAttempt{{RawUsage: []json.RawMessage{json.RawMessage(`{"prompt_tokens":100,"completion_tokens":2,"total_tokens":102,"prompt_tokens_details":{"cached_tokens":0},"completion_tokens_details":{"reasoning_tokens":1}}`)}}}}
	cacheDiagnosticCompare(r)
	if r.UsageMatch != "mismatch" {
		t.Fatal("reasoning usage mismatch was missed")
	}
	r.SDKUsage.CompletionTokensDetails.ReasoningTokens = 1
	cacheDiagnosticCompare(r)
	if r.UsageMatch != "match" {
		t.Fatal("matching reasoning usage rejected")
	}
}
