package aihelper

import (
	"GoAI/config"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ollama"
	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// StreamCallback 流式回调函数
type StreamCallback func(msg string)

// AIModel 定义AI模型接口
type AIModel interface {
	GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error)
	StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error)
	GetModelType() string
}

// filterThinkingTags 过滤思考标签，将思考过程替换为空白
func filterThinkingTags(content string) string {
	// 匹配 deepseek <think>...</think> 标签及其内容（包括多行）
	// re := regexp.MustCompile(`(?s)<think>.*?</think>\n\n`)
	// 将匹配到的内容（包括可能的换行符）替换为空字符串
	// filtered := re.ReplaceAllString(content, "")
	// return filtered
	return content
}

const ragLogPreviewRunes = 120

func previewRAGQueryForLog(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	runes := []rune(text)
	if len(runes) <= ragLogPreviewRunes {
		return text
	}
	return string(runes[:ragLogPreviewRunes]) + "..."
}

// =================== Qwen类型接口的实现 ===================
type QwenModel struct {
	llm model.ToolCallingChatModel
}

// NewQwenModel 创建并初始化一个新的Qwen模型实例
// 参数:
//
//	ctx context.Context - 上下文对象
//
// 返回:
//
//	*QwenModel - 初始化后的Qwen模型实例
//	error - 如果初始化过程中出现错误则返回错误信息
func NewQwenModel(ctx context.Context) (*QwenModel, error) {
	conf := config.GetConfig()
	apiKey := conf.QwenConfig.QwenApiKey
	modelName := conf.QwenConfig.QwenModelName
	baseURL := conf.QwenConfig.QwenBaseUrl
	enableThinking := conf.QwenConfig.EnableThinking

	chatConfig := &qwen.ChatModelConfig{
		BaseURL:        baseURL,
		Model:          modelName,
		APIKey:         apiKey,
		EnableThinking: &enableThinking,
	}

	llm, err := qwen.NewChatModel(ctx, chatConfig)
	if err != nil {
		return nil, fmt.Errorf("create qwen model failed: %v", err)
	}
	return &QwenModel{llm: llm}, nil
}

// GenerateResponse 使用 Qwen 模型生成对话响应
// 参数:
//
//	ctx: 上下文对象
//	messages: 消息列表
//
// 返回:
//
//	*schema.Message: 生成的响应消息
//	error: 错误信息
func (o *QwenModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("qwen generate failed: %v", err)
	}
	return resp, nil
}

// StreamResponse 以流式方式处理Qwen模型的响应，接收消息列表并通过回调函数实时返回部分结果
// ctx: 上下文用于控制请求生命周期
// messages: 要发送给Qwen的消息列表
// cb: 流式回调函数，用于实时处理接收到的部分响应内容
// 返回值: 完整的响应内容和可能发生的错误
func (o *QwenModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("qwen stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("qwen stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content) // 聚合
			cb(msg.Content)                   // 实时调用cb函数，方便主动发送给前端
		}
	}

	return fullResp.String(), nil //返回完整内容，方便后续存储
}

func (o *QwenModel) GetModelType() string { return "qwen" }

// =================== Ollama 实现 ===================
type OllamaModel struct {
	llm model.ToolCallingChatModel
}

// NewOllamaModel 创建并返回一个新的OllamaModel实例
// 参数:
//
//	ctx context.Context - 上下文对象
//
// 返回值:
//
//	*OllamaModel - 创建的OllamaModel实例
//	error - 创建过程中遇到的错误
func NewOllamaModel(ctx context.Context) (*OllamaModel, error) {
	conf := config.GetConfig()
	modelName := conf.OllamaConfig.OllamaModelName
	baseURL := conf.OllamaConfig.OllamaBaseUrl

	chatConfig := &ollama.ChatModelConfig{
		BaseURL:  baseURL,
		Model:    modelName,
		Thinking: &ollama.ThinkValue{Value: conf.OllamaConfig.EnableThinking},
	}

	llm, err := ollama.NewChatModel(ctx, chatConfig)
	if err != nil {
		return nil, fmt.Errorf("create ollama model failed: %v", err)
	}
	return &OllamaModel{llm: llm}, nil
}

// GenerateResponse 使用Ollama模型生成对话响应，接收消息上下文并返回生成的响应消息
// 参数:
//
//	ctx: 上下文对象，用于控制请求生命周期
//	messages: 输入的消息历史记录
//
// 返回值:
//
//	*schema.Message: 生成的响应消息
//	error: 生成过程中出现的错误
//
// 注意: 会自动过滤响应内容中的思考标签(thinking tags)
func (o *OllamaModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	resp, err := o.llm.Generate(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("ollama generate failed: %v", err)
	}

	// 过滤思考标签
	if resp != nil && len(resp.Content) > 0 {
		resp.Content = filterThinkingTags(resp.Content)
	}

	return resp, nil
}

// StreamResponse 处理Ollama模型的流式响应，接收消息列表并通过回调函数实时返回内容
// 参数:
//
//	ctx: 上下文对象
//	messages: 要处理的消息列表
//	cb: 流式回调函数，用于实时发送内容到前端
//
// 返回值:
//
//	string: 过滤后的完整响应内容
//	error: 错误信息
//
// 功能:
//   - 处理流式响应，自动过滤<think>标签内的内容
//   - 通过回调函数实时推送非思考内容
//   - 返回经过二次过滤的完整响应内容
func (o *OllamaModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		return "", fmt.Errorf("ollama stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder
	var thinkingBuffer strings.Builder // 用于缓存思考标签内的内容
	var inThinkingBlock bool           // 标记是否在思考标签内

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			// 流结束，如果有缓存的思考内容，不添加到最终响应中
			break
		}
		if err != nil {
			return "", fmt.Errorf("ollama stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			content := msg.Content

			// 处理思考标签
			if strings.Contains(content, "<think>") {
				inThinkingBlock = true
			}

			if strings.Contains(content, "</think>") {
				inThinkingBlock = false
				// 重置思考缓冲区
				thinkingBuffer.Reset()
				continue // 跳过思考标签结束标记
			}

			// 如果不在思考标签内，则处理内容
			if !inThinkingBlock {
				fullResp.WriteString(content) // 聚合
				cb(content)                   // 实时调用cb函数，方便主动发送给前端
			} else {
				// 在思考标签内，将内容添加到思考缓冲区（不发送给前端）
				thinkingBuffer.WriteString(content)
			}
		}
	}
	// 对完整响应进行二次过滤，确保没有遗漏的思考标签
	finalContent := filterThinkingTags(fullResp.String())
	return finalContent, nil //返回完整内容，方便后续存储
}

func (o *OllamaModel) GetModelType() string { return "ollama" }

// =================== RAG 实现 ===================
type AliRAGModel struct {
	llm                 model.ToolCallingChatModel
	rewriteLLM          model.ToolCallingChatModel
	uid                 string // 用于获取用户的文档
	queryRewriteEnabled bool
	queryRewriteTimeout time.Duration
	ragGenerateWorkflow compose.Runnable[[]*schema.Message, *schema.Message]
	ragPrepareWorkflow  compose.Runnable[[]*schema.Message, *ragWorkflowState]
	newRAGQuery         ragNewQueryFunc
}

// NewAliRAGModel 创建并返回一个新的AliRAGModel实例
// ctx: 上下文对象，用于控制请求的生命周期
// uid: 用户 UID，将与模型关联
// 返回: *AliRAGModel实例和可能的错误信息
func NewAliRAGModel(ctx context.Context, uid string) (*AliRAGModel, error) {
	conf := config.GetConfig()
	apiKey := conf.RagModelConfig.RagApiKey
	modelName := conf.RagModelConfig.RagChatModelName
	baseURL := conf.RagModelConfig.RagBaseUrl
	enableThinking := conf.RagModelConfig.EnableThinking

	chatConfig := &qwen.ChatModelConfig{
		BaseURL:        baseURL,
		Model:          modelName,
		APIKey:         apiKey,
		EnableThinking: &enableThinking,
	}

	llm, err := qwen.NewChatModel(ctx, chatConfig)
	if err != nil {
		return nil, fmt.Errorf("create ali rag model failed: %v", err)
	}

	var rewriteLLM model.ToolCallingChatModel
	queryRewriteEnabled := conf.RagModelConfig.QueryRewriteEnabled
	rewriteTimeout := time.Duration(conf.RagModelConfig.QueryRewriteTimeout) * time.Second
	if rewriteTimeout <= 0 {
		rewriteTimeout = 8 * time.Second
	}
	if queryRewriteEnabled {
		rewriteModelName := strings.TrimSpace(conf.RagModelConfig.QueryRewriteModel)
		if rewriteModelName == "" {
			rewriteModelName = modelName
		}
		rewriteThinking := false
		rewriteConfig := &qwen.ChatModelConfig{
			BaseURL:        baseURL,
			Model:          rewriteModelName,
			APIKey:         apiKey,
			EnableThinking: &rewriteThinking,
		}
		rewriteLLM, err = qwen.NewChatModel(ctx, rewriteConfig)
		if err != nil {
			log.Printf("[RAG][Rewrite] init failed uid=%s model=%s fallback=original_query err=%v", uid, rewriteModelName, err)
			queryRewriteEnabled = false
		}
	}
	ragModel := &AliRAGModel{
		llm:                 llm,
		rewriteLLM:          rewriteLLM,
		uid:                 uid,
		queryRewriteEnabled: queryRewriteEnabled,
		queryRewriteTimeout: rewriteTimeout,
		newRAGQuery:         defaultRAGNewQuery,
	}
	if err := ragModel.compileRAGWorkflows(ctx); err != nil {
		return nil, err
	}
	return ragModel, nil
}

func (o *AliRAGModel) rewriteRAGQuery(ctx context.Context, messages []*schema.Message, originalQuery string) string {
	originalQuery = strings.TrimSpace(originalQuery)
	if originalQuery == "" || !o.queryRewriteEnabled || o.rewriteLLM == nil {
		return originalQuery
	}

	rewriteCtx, cancel := context.WithTimeout(ctx, o.queryRewriteTimeout)
	defer cancel()

	prompt := buildRAGRewritePrompt(messages)
	rewriteStart := time.Now()
	resp, err := o.rewriteLLM.Generate(rewriteCtx, []*schema.Message{
		{Role: schema.User, Content: prompt},
	})
	if err != nil {
		log.Printf("[RAG][Rewrite] failed uid=%s fallback=original_query err=%v elapsed=%s", o.uid, err, time.Since(rewriteStart))
		return originalQuery
	}
	rewritten := ""
	if resp != nil {
		rewritten = sanitizeRewrittenQuery(resp.Content)
	}
	if rewritten == "" {
		log.Printf("[RAG][Rewrite] empty uid=%s fallback=original_query elapsed=%s", o.uid, time.Since(rewriteStart))
		return originalQuery
	}
	log.Printf("[RAG][Rewrite] success uid=%s original_len=%d rewritten_len=%d elapsed=%s rewritten_preview=%q", o.uid, len(originalQuery), len(rewritten), time.Since(rewriteStart), previewRAGQueryForLog(rewritten))
	return rewritten
}

func buildRAGRewritePrompt(messages []*schema.Message) string {
	recent := recentRAGMessages(messages, 6)
	var history strings.Builder
	for _, msg := range recent {
		if msg == nil {
			continue
		}
		role := "用户"
		if msg.Role != schema.User {
			role = "助手"
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		history.WriteString(role)
		history.WriteString("：")
		history.WriteString(content)
		history.WriteString("\n")
	}

	return fmt.Sprintf(`你是 RAG 检索 query 改写器。请根据最近对话，把最后一个用户问题改写成一个独立、清晰、适合检索知识库的问题。
要求：
1. 只输出改写后的检索问题，不要解释，不要回答问题。
2. 消解“它、这个、那个、上面、刚才、前者、后者”等指代。
3. 保留原问题中的实体、文件名、时间、数字、指标、限定条件。
4. 口语化表达要改成文档中更可能出现的正式关键词。
5. 如果最后一个用户问题已经清晰，原样输出。
6. 保留“我的、父亲的、家属的”等病历归属及人物关系；结合用户最近的明确说明消解指代，不得改写为泛化的类似病例查询，不得将助手猜测的归属当作用户确认。

最近对话：
%s
改写后的检索问题：`, strings.TrimSpace(history.String()))
}

func recentRAGMessages(messages []*schema.Message, limit int) []*schema.Message {
	if limit <= 0 || len(messages) <= limit {
		return messages
	}
	return messages[len(messages)-limit:]
}

func sanitizeRewrittenQuery(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"改写后的检索问题：", "改写后的检索问题:", "检索问题：", "检索问题:", "query:", "Query:"} {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	text = strings.Trim(text, "\"'“”‘’`")
	text = strings.TrimSpace(text)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, "\"'“”‘’`")
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// GenerateResponse 使用RAG(检索增强生成)技术生成对话响应。首先尝试检索与用户问题相关的文档，
// 如果检索失败或用户未上传文档，则直接使用原始问题调用LLM生成回答。
//
// 参数:
//
//	ctx: 上下文对象
//	messages: 对话消息历史，最后一条消息作为当前查询
//
// 返回:
//
//	*schema.Message: 生成的响应消息
//	error: 错误信息，生成失败时返回
func (o *AliRAGModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	start := time.Now()
	log.Printf("[RAG][Generate] start uid=%s messages=%d workflow=true", o.uid, len(messages))

	resp, err := o.ragGenerateWorkflow.Invoke(ctx, messages)
	if err != nil {
		log.Printf("[RAG][Generate] workflow failed uid=%s err=%v elapsed=%s", o.uid, err, time.Since(start))
		return nil, fmt.Errorf("ali rag generate failed: %v", err)
	}
	respLen := 0
	if resp != nil {
		respLen = len(resp.Content)
	}
	log.Printf("[RAG][Generate] success uid=%s response_len=%d total_elapsed=%s", o.uid, respLen, time.Since(start))
	return resp, nil
}

// llmStream 处理不带RAG的流式响应
// 参数:
//
//	ctx: 上下文
//	messages: 消息列表
//	cb: 流式回调函数
//
// 返回值:
//
//	string: 完整响应内容
//	error: 错误信息
func (o *AliRAGModel) llmStream(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	start := time.Now()
	log.Printf("[RAG][Stream][LLM] start uid=%s messages=%d", o.uid, len(messages))

	stream, err := o.llm.Stream(ctx, messages)
	if err != nil {
		log.Printf("[RAG][Stream][LLM] start failed uid=%s err=%v", o.uid, err)
		return "", fmt.Errorf("ali rag stream failed: %v", err)
	}
	defer stream.Close()

	var fullResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[RAG][Stream][LLM] recv failed uid=%s err=%v elapsed=%s", o.uid, err, time.Since(start))
			return "", fmt.Errorf("ali rag stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			fullResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	result := fullResp.String()
	log.Printf("[RAG][Stream][LLM] success uid=%s response_len=%d elapsed=%s", o.uid, len(result), time.Since(start))
	return result, nil
}

// StreamResponse 使用RAG(检索增强生成)技术处理消息流并返回响应
//
// 参数:
//
//	ctx: 上下文对象，用于控制请求生命周期
//	messages: 消息历史记录，最后一条消息将作为查询
//	cb: 流式回调函数，用于实时接收响应片段
//
// 返回值:
//
//	string: 完整的响应内容
//	error: 执行过程中发生的错误，nil表示成功
//
// 处理流程:
//  1. 创建RAG查询器
//  2. 从消息历史中提取查询内容
//  3. 检索相关文档
//  4. 构建包含检索结果的提示词
//  5. 流式调用LLM生成响应
//
// 注意:
//   - 如果RAG查询或文档检索失败，将回退到不使用RAG的原始流式处理
//   - 回调函数cb会实时接收流式响应片段
func (o *AliRAGModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	start := time.Now()
	log.Printf("[RAG][Stream] start uid=%s messages=%d workflow=true", o.uid, len(messages))

	state, err := o.ragPrepareWorkflow.Invoke(ctx, messages)
	if err != nil {
		log.Printf("[RAG][Stream] workflow prepare failed uid=%s err=%v elapsed=%s", o.uid, err, time.Since(start))
		return "", err
	}
	ragMessages := state.MessagesForLLM
	if len(ragMessages) == 0 {
		ragMessages = messages
	}
	content, streamErr := o.llmStream(ctx, ragMessages, cb)
	log.Printf("[RAG][Stream] finished uid=%s docs=%d fallback=%v response_len=%d err=%v total_elapsed=%s", o.uid, len(state.Docs), state.Fallback, len(content), streamErr, time.Since(start))
	return content, streamErr
}

func (o *AliRAGModel) GetModelType() string { return "rag" }

// =================== MCP 实现 ===================
// MCPModel MCP模型实现，集成MCP服务
type MCPModel struct {
	llm        model.ToolCallingChatModel
	mcpClient  *client.Client
	mcpBaseURL string
}

// AIToolCall 表示AI工具调用请求
type AIToolCall struct {
	IsToolCall bool                   `json:"isToolCall"`
	ToolName   string                 `json:"toolName"`
	Args       map[string]interface{} `json:"args"`
}

// NewMCPModel 创建MCP模型实例
// 从配置中读取Qwen API密钥、模型名称、基础URL和MCP基础URL
// 初始化Qwen聊天模型并封装到MCPModel结构体中返回
func NewMCPModel(ctx context.Context) (*MCPModel, error) {
	conf := config.GetConfig()
	apiKey := conf.QwenConfig.QwenApiKey
	modelName := conf.RagModelConfig.RagChatModelName
	baseURL := conf.RagModelConfig.RagBaseUrl
	mcpBaseURL := conf.MCPConfig.MCPBaseUrl
	enableThinking := conf.QwenConfig.EnableThinking

	chatConfig := &qwen.ChatModelConfig{
		BaseURL:        baseURL,
		Model:          modelName,
		APIKey:         apiKey,
		EnableThinking: &enableThinking,
	}

	llm, err := qwen.NewChatModel(ctx, chatConfig)
	if err != nil {
		return nil, fmt.Errorf("create mcp model failed: %v", err)
	}

	return &MCPModel{
		llm:        llm,
		mcpBaseURL: mcpBaseURL,
	}, nil
}

// getMCPClient 获取或创建 MCP 客户端实例
// 如果客户端尚未初始化，则创建新的 HTTP 传输层并初始化客户端
// 参数 ctx 为上下文，用于控制请求生命周期
// 返回已初始化的 MCP 客户端实例，如果初始化失败则返回错误
func (m *MCPModel) getMCPClient(ctx context.Context) (*client.Client, error) {
	if m.mcpClient == nil {
		// 创建 MCP 客户端
		httpTransport, err := transport.NewStreamableHTTP(m.mcpBaseURL)
		if err != nil {
			return nil, fmt.Errorf("create mcp transport failed: %v", err)
		}

		m.mcpClient = client.NewClient(httpTransport)

		// 初始化 MCP 客户端
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "MCP-Go AIHelper Client",
			Version: "1.0.0",
		}
		initRequest.Params.Capabilities = mcp.ClientCapabilities{}

		if _, err := m.mcpClient.Initialize(ctx, initRequest); err != nil {
			return nil, fmt.Errorf("mcp client initialize failed: %v", err)
		}
	}
	return m.mcpClient, nil
}

// GenerateResponse 生成响应，支持MCP工具调用
// 首先调用LLM判断是否需要使用工具，如果需要则调用MCP工具并将结果返回给LLM生成最终响应
// 参数:
//   - ctx: 上下文
//   - messages: 消息列表
//
// 返回:
//   - *schema.Message: 生成的响应消息
//   - error: 错误信息
func (m *MCPModel) GenerateResponse(ctx context.Context, messages []*schema.Message) (*schema.Message, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}

	// 获取最后一条消息
	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	// 第一次调用AI：告诉AI使用固定的JSON格式
	firstPrompt := m.buildFirstPrompt(query)
	concatMessages := make([]*schema.Message, len(messages))
	copy(concatMessages, messages)
	concatMessages[len(concatMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: firstPrompt,
	}

	// 调用LLM生成第一次响应
	firstResp, err := m.llm.Generate(ctx, concatMessages)
	if err != nil {
		return nil, fmt.Errorf("mcp first generate failed: %v", err)
	}
	log.Println("[GenerateResponse] first resp is ", firstResp)
	// 解析AI响应
	aiResult := firstResp.Content
	toolCall, err := m.parseAIResponse(aiResult)
	if err != nil {
		log.Printf("Failed to parse AI response: %v", err)
		return firstResp, nil
	}

	// 情况1：AI不调用工具，直接返回响应
	if !toolCall.IsToolCall {
		log.Println("[GenerateResponse] toolCall IsToolCall is false, ", firstResp)
		return firstResp, nil
	}
	log.Println("[GenerateResponse] toolCall IsToolCall is true, ", firstResp)
	// 情况2：AI要调用工具
	// 获取MCP客户端
	mcpClient, err := m.getMCPClient(ctx)
	if err != nil {
		log.Printf("MCP client error: %v", err)
		return firstResp, nil
	}

	// 调用MCP工具
	toolResult, err := m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
	if err != nil {
		log.Printf("MCP tool call failed: %v", err)
		return firstResp, nil
	}

	// 第二次调用AI：将工具结果告诉AI
	secondPrompt := m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult)
	// concatMessages = make([]*schema.Message, len(messages))
	// copy(concatMessages, messages)
	concatMessages[len(concatMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: secondPrompt,
	}

	// 调用LLM生成最终响应
	finalResp, err := m.llm.Generate(ctx, concatMessages)
	if err != nil {
		return nil, fmt.Errorf("mcp second generate failed: %v", err)
	}
	log.Println("[GenerateResponse] final resp is ", finalResp)
	return finalResp, nil
}

// StreamResponse 流式响应，集成MCP工具
// 首先调用LLM判断是否需要使用MCP工具，如果需要则调用工具后再次调用LLM生成最终响应
// 参数:
//   - ctx: 上下文
//   - messages: 消息列表
//   - cb: 流式回调函数
//
// 返回:
//   - string: 最终响应内容
//   - error: 错误信息
func (m *MCPModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages provided")
	}

	// 获取最后一条消息
	lastMessage := messages[len(messages)-1]
	query := lastMessage.Content

	// 第一次调用AI：告诉AI使用固定的JSON格式
	firstPrompt := m.buildFirstPrompt(query)
	concatMessages := make([]*schema.Message, len(messages))
	copy(concatMessages, messages)
	concatMessages[len(concatMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: firstPrompt,
	}

	// 第一次调用LLM，使用流式接口
	stream, err := m.llm.Stream(ctx, concatMessages)
	if err != nil {
		return "", fmt.Errorf("mcp first stream failed: %v", err)
	}
	defer stream.Close()

	var firstContent strings.Builder
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("mcp first stream recv failed: %v", err)
		}
		firstContent.WriteString(msg.Content)
		cb(msg.Content)
	}

	firstResp := &schema.Message{Role: schema.Assistant, Content: firstContent.String()}

	// 第一次调用LLM，使用同步接口（非流式）
	// firstResp, err := m.llm.Generate(ctx, concatMessages)
	// if err != nil {
	// 	return "", fmt.Errorf("mcp first generate failed: %v", err)
	// }
	// log.Println("[StreamResponse] first resp is ", firstResp)

	// 解析AI响应
	aiResult := firstResp.Content
	toolCall, err := m.parseAIResponse(aiResult)
	if err != nil {
		log.Printf("Failed to parse AI response: %v", err)
		return aiResult, nil
	}

	// 情况1：AI不调用工具，直接返回响应
	if !toolCall.IsToolCall {
		log.Println("[StreamResponse] toolCall IsToolCall is false, ", firstResp)
		cb(aiResult) // 将结果发给前端
		return aiResult, nil
	}
	log.Println("[StreamResponse] toolCall IsToolCall is true, ", firstResp)
	// 情况2：AI要调用工具
	// 获取MCP客户端
	mcpClient, err := m.getMCPClient(ctx)
	if err != nil {
		log.Printf("MCP client error: %v", err)
		return aiResult, nil
	}

	// 调用MCP工具
	toolResult, err := m.callMCPTool(ctx, mcpClient, toolCall.ToolName, toolCall.Args)
	if err != nil {
		log.Printf("MCP tool call failed: %v", err)
		return aiResult, nil
	}

	// 第二次调用LLM，将工具结果告诉AI
	secondPrompt := m.buildSecondPrompt(query, toolCall.ToolName, toolCall.Args, toolResult)
	// concatMessages = make([]*schema.Message, len(messages))
	// copy(concatMessages, messages)
	concatMessages[len(concatMessages)-1] = &schema.Message{
		Role:    schema.User,
		Content: secondPrompt,
	}

	// LLM生成最终响应（流式）
	stream, err = m.llm.Stream(ctx, concatMessages)
	if err != nil {
		return "", fmt.Errorf("mcp second stream failed: %v", err)
	}
	defer stream.Close()

	var finalResp strings.Builder

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("mcp second stream recv failed: %v", err)
		}
		if len(msg.Content) > 0 {
			finalResp.WriteString(msg.Content)
			cb(msg.Content)
		}
	}

	return finalResp.String(), nil
}

// buildFirstPrompt 构建MCP模型的首次提示词，包含可用工具说明和调用规则
// 告诉ai有哪些能力，让ai来判断，用户的问题是否需要调用这些工具
func (m *MCPModel) buildFirstPrompt(query string) string {
	return fmt.Sprintf(`你是一个智能助手，可以调用MCP工具来获取信息。

可用工具:
- get_weather: 获取指定城市的天气信息，参数: city（城市名称，支持中文和英文，如北京、Shanghai等）

重要规则:
1. 如果需要调用工具，必须严格返回以下JSON格式：
{
  "isToolCall": true,
  "toolName": "工具名称",
  "args": {"参数名": "参数值"}
}
2. 如果不需要调用工具，直接返回自然语言回答
3. 请根据用户问题决定是否需要调用工具

用户问题: %s

请根据需要调用适当的工具，然后给出综合的回答。`, query)
}

// buildSecondPrompt 构建用于生成最终回答的提示词，整合工具执行结果和用户问题
// 将工具结果告诉AI
func (m *MCPModel) buildSecondPrompt(query, toolName string, args map[string]interface{}, toolResult string) string {
	return fmt.Sprintf(`你是一个智能助手，可以调用MCP工具来获取信息。

工具执行结果:
工具名称: %s
工具参数: %v
工具结果: %s

用户问题: %s

请根据工具结果和用户问题，给出最终的综合回答。`, toolName, args, toolResult, query)
}

// parseAIResponse 解析AI响应，检查是否包含工具调用
func (m *MCPModel) parseAIResponse(response string) (*AIToolCall, error) {
	// 尝试解析为JSON
	var toolCall AIToolCall
	if err := json.Unmarshal([]byte(response), &toolCall); err == nil {
		return &toolCall, nil
	}

	// 如果不是JSON，检查是否包含工具调用关键词
	if strings.Contains(response, "get_weather") {
		// 尝试提取城市名称
		city := m.extractCityFromResponse(response)
		if city != "" {
			return &AIToolCall{
				IsToolCall: true,
				ToolName:   "get_weather",
				Args:       map[string]interface{}{"city": city},
			}, nil
		}
	}

	// 不是工具调用
	return &AIToolCall{IsToolCall: false}, nil
}

// callMCPTool 调用MCP工具
func (m *MCPModel) callMCPTool(ctx context.Context, client *client.Client, toolName string, args map[string]interface{}) (string, error) {
	callToolRequest := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}

	result, err := client.CallTool(ctx, callToolRequest)
	if err != nil {
		return "", fmt.Errorf("mcp tool call failed: %v", err)
	}

	// 提取工具结果文本
	var text string
	for _, content := range result.Content {
		if textContent, ok := content.(mcp.TextContent); ok {
			text += textContent.Text + "\n"
		}
	}

	return text, nil
}

// extractCityFromResponse 从响应中提取城市名称
// 直接从AI返回的JSON中提取城市，不预留城市列表
func (m *MCPModel) extractCityFromResponse(response string) string {
	// 尝试从JSON中提取城市
	var toolCall AIToolCall
	if err := json.Unmarshal([]byte(response), &toolCall); err == nil {
		if args, ok := toolCall.Args["city"].(string); ok {
			return args
		}
	}

	// 如果JSON解析失败，尝试从文本中提取城市名称
	// 这部分可以根据实际需要扩展，但不再预留固定城市列表
	return ""
}

// GetModelType 获取模型类型
func (m *MCPModel) GetModelType() string { return "mcp" }

// Close 关闭MCP客户端
func (m *MCPModel) Close() {
	if m.mcpClient != nil {
		m.mcpClient.Close()
	}
}
