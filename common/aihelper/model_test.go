package aihelper

import (
	"context"
	"fmt"
	"strings"
	"testing"

	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestBuildRAGRewritePromptUsesRecentMessages(t *testing.T) {
	messages := []*schema.Message{
		{Role: schema.User, Content: "第一轮问题"},
		{Role: schema.Assistant, Content: "第一轮回答"},
		{Role: schema.User, Content: "第二轮问题"},
		{Role: schema.Assistant, Content: "第二轮回答"},
		{Role: schema.User, Content: "第三轮问题"},
		{Role: schema.Assistant, Content: "第三轮回答"},
		{Role: schema.User, Content: "那它的就业率呢"},
	}

	prompt := buildRAGRewritePrompt(messages)

	if strings.Contains(prompt, "第一轮问题") {
		t.Fatalf("prompt should only include recent messages: %s", prompt)
	}
	if !strings.Contains(prompt, "那它的就业率呢") {
		t.Fatalf("prompt should include latest question: %s", prompt)
	}
	if !strings.Contains(prompt, "只输出改写后的检索问题") {
		t.Fatalf("prompt should constrain output: %s", prompt)
	}
}

func TestSanitizeRewrittenQueryStripsPrefixesAndQuotes(t *testing.T) {
	got := sanitizeRewrittenQuery("检索问题：\"2023届就业质量报告 就业率\"\n说明：忽略")

	if got != "2023届就业质量报告 就业率" {
		t.Fatalf("sanitized query: got %q", got)
	}
}

func TestRAGWorkflowPrepareRejectsEmptyMessages(t *testing.T) {
	model := newTestAliRAGModel(t, &testChatModel{response: "unused"}, nil)

	if _, err := model.ragPrepareWorkflow.Invoke(context.Background(), nil); err == nil {
		t.Fatalf("expected empty messages to fail")
	}
}

func TestRAGWorkflowBuildsRAGPromptWhenDocsRetrieved(t *testing.T) {
	chatModel := &testChatModel{response: "answer"}
	model := newTestAliRAGModel(t, chatModel, func(ctx context.Context, uid string) (ragRetrieveDocumentsFunc, error) {
		return func(ctx context.Context, query string) ([]*schema.Document, error) {
			if query != "2023毕业人数是多少" {
				t.Fatalf("retrieval query: got %q", query)
			}
			return []*schema.Document{{
				ID:      "doc_1",
				Content: "2023毕业人数为100人。",
				MetaData: map[string]any{
					"original_name": "就业质量报告.md",
				},
			}}, nil
		}, nil
	})

	state, err := model.ragPrepareWorkflow.Invoke(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "2023毕业人数是多少"},
	})
	if err != nil {
		t.Fatalf("prepare workflow failed: %v", err)
	}
	if state.Fallback {
		t.Fatalf("expected rag path, got fallback reason=%s", state.FallbackReason)
	}
	if len(state.MessagesForLLM) != 1 {
		t.Fatalf("messages for llm: got %d", len(state.MessagesForLLM))
	}
	prompt := state.MessagesForLLM[0].Content
	if !strings.Contains(prompt, "2023毕业人数为100人。") {
		t.Fatalf("prompt should include retrieved doc: %s", prompt)
	}
	if !strings.Contains(prompt, "用户原始问题：2023毕业人数是多少") {
		t.Fatalf("prompt should keep original question: %s", prompt)
	}
}

func TestRAGWorkflowFallsBackToOriginalMessagesWhenQueryInitFails(t *testing.T) {
	chatModel := &testChatModel{response: "answer"}
	model := newTestAliRAGModel(t, chatModel, func(ctx context.Context, uid string) (ragRetrieveDocumentsFunc, error) {
		return nil, fmt.Errorf("no files")
	})
	messages := []*schema.Message{{Role: schema.User, Content: "普通问题"}}

	state, err := model.ragPrepareWorkflow.Invoke(context.Background(), messages)
	if err != nil {
		t.Fatalf("prepare workflow failed: %v", err)
	}
	if !state.Fallback || state.FallbackReason != "query_init_failed" {
		t.Fatalf("fallback state: fallback=%v reason=%s", state.Fallback, state.FallbackReason)
	}
	if state.MessagesForLLM[0] != messages[0] {
		t.Fatalf("fallback should keep original message")
	}
}

func TestRAGWorkflowFallsBackToOriginalMessagesWhenRetrieveFails(t *testing.T) {
	chatModel := &testChatModel{response: "answer"}
	model := newTestAliRAGModel(t, chatModel, func(ctx context.Context, uid string) (ragRetrieveDocumentsFunc, error) {
		return func(ctx context.Context, query string) ([]*schema.Document, error) {
			return nil, fmt.Errorf("redis search failed")
		}, nil
	})
	messages := []*schema.Message{{Role: schema.User, Content: "问题"}}

	state, err := model.ragPrepareWorkflow.Invoke(context.Background(), messages)
	if err != nil {
		t.Fatalf("prepare workflow failed: %v", err)
	}
	if !state.Fallback || state.FallbackReason != "retrieve_failed" {
		t.Fatalf("fallback state: fallback=%v reason=%s", state.Fallback, state.FallbackReason)
	}
	if state.MessagesForLLM[0] != messages[0] {
		t.Fatalf("fallback should keep original message")
	}
}

func TestRAGGenerateResponseUsesWorkflow(t *testing.T) {
	chatModel := &testChatModel{response: "workflow answer"}
	model := newTestAliRAGModel(t, chatModel, func(ctx context.Context, uid string) (ragRetrieveDocumentsFunc, error) {
		return func(ctx context.Context, query string) ([]*schema.Document, error) {
			return []*schema.Document{{Content: "上下文内容", MetaData: map[string]any{"knowledge_scope": "personal"}}}, nil
		}, nil
	})

	resp, err := model.GenerateResponse(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "问题"},
	})
	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}
	if resp.Content != "workflow answer" {
		t.Fatalf("response: got %q", resp.Content)
	}
	if len(chatModel.lastGenerateInput) != 1 || (!strings.Contains(chatModel.lastGenerateInput[0].Content, "上下文内容") || !strings.Contains(chatModel.lastGenerateInput[0].Content, "资料来源：当前用户个人库")) {
		t.Fatalf("chat model should receive rag prompt, got %#v", chatModel.lastGenerateInput)
	}
}

func TestRAGStreamResponseUsesPreparedWorkflowMessages(t *testing.T) {
	chatModel := &testChatModel{streamChunks: []string{"chunk1", "chunk2"}}
	model := newTestAliRAGModel(t, chatModel, func(ctx context.Context, uid string) (ragRetrieveDocumentsFunc, error) {
		return func(ctx context.Context, query string) ([]*schema.Document, error) {
			return []*schema.Document{{Content: "流式上下文", MetaData: map[string]any{"knowledge_scope": "personal"}}}, nil
		}, nil
	})

	var streamed strings.Builder
	content, err := model.StreamResponse(context.Background(), []*schema.Message{
		{Role: schema.User, Content: "问题"},
	}, func(msg string) {
		streamed.WriteString(msg)
	})
	if err != nil {
		t.Fatalf("StreamResponse failed: %v", err)
	}
	if content != "chunk1chunk2" || streamed.String() != content {
		t.Fatalf("streamed content mismatch: content=%q streamed=%q", content, streamed.String())
	}
	if len(chatModel.lastStreamInput) != 1 || (!strings.Contains(chatModel.lastStreamInput[0].Content, "流式上下文") || !strings.Contains(chatModel.lastStreamInput[0].Content, "资料来源：当前用户个人库")) {
		t.Fatalf("stream should receive rag prompt, got %#v", chatModel.lastStreamInput)
	}
}

func newTestAliRAGModel(t *testing.T, chatModel *testChatModel, newQuery ragNewQueryFunc) *AliRAGModel {
	t.Helper()
	model := &AliRAGModel{
		llm:                 chatModel,
		uid:                 "tester",
		queryRewriteEnabled: false,
		newRAGQuery:         newQuery,
	}
	if err := model.compileRAGWorkflows(context.Background()); err != nil {
		t.Fatalf("compile workflow failed: %v", err)
	}
	return model
}

type testChatModel struct {
	response          string
	lastGenerateInput []*schema.Message
	lastStreamInput   []*schema.Message
	streamChunks      []string
}

func (m *testChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...einoModel.Option) (*schema.Message, error) {
	m.lastGenerateInput = input
	return &schema.Message{Role: schema.Assistant, Content: m.response}, nil
}

func (m *testChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...einoModel.Option) (*schema.StreamReader[*schema.Message], error) {
	m.lastStreamInput = input
	chunks := m.streamChunks
	if len(chunks) == 0 {
		chunks = []string{m.response}
	}
	reader, writer := schema.Pipe[*schema.Message](len(chunks))
	go func() {
		defer writer.Close()
		for _, chunk := range chunks {
			writer.Send(&schema.Message{Role: schema.Assistant, Content: chunk}, nil)
		}
	}()
	return reader, nil
}

func (m *testChatModel) WithTools(tools []*schema.ToolInfo) (einoModel.ToolCallingChatModel, error) {
	return m, nil
}

func TestRewritePromptPreservesMedicalRecordOwnership(t *testing.T) {
	prompt := buildRAGRewritePrompt([]*schema.Message{
		{Role: schema.User, Content: "这是我父亲的病历"},
		{Role: schema.Assistant, Content: "请继续"},
		{Role: schema.User, Content: "他的检查结果是什么"},
	})
	for _, want := range []string{"这是我父亲的病历", "他的检查结果是什么", "保留“我的、父亲的、家属的”等病历归属", "不得改写为泛化的类似病例查询"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing context or constraint %q", want)
		}
	}
}
