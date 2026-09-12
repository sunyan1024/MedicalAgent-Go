package session

import (
	"GoAI/common/code"
	"GoAI/controller"
	medical "GoAI/controller/medical"
	"GoAI/model"
	"GoAI/service/session"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

type (
	GetUserSessionsResponse struct {
		controller.Response                     // 嵌入基础响应结构
		Sessions            []model.SessionInfo `json:"sessions,omitempty"`
	}

	CreateSessionAndSendMessageRequest struct {
		ReportID     string `json:"reportId,omitempty"`
		UserQuestion string `json:"question" binding:"required"`  // 用户问题;
		ModelType    string `json:"modelType" binding:"required"` // 模型类型;
	}

	CreateSessionAndSendMessageResponse struct {
		AiInformation string `json:"Information,omitempty"` // AI回答
		SessionID     string `json:"sessionId,omitempty"`   // 当前会话ID
		controller.Response
	}

	ChatSendRequest struct {
		ReportID     string `json:"reportId,omitempty"`
		UserQuestion string `json:"question" binding:"required"`            // 用户问题;
		ModelType    string `json:"modelType" binding:"required"`           // 模型类型;
		SessionID    string `json:"sessionId,omitempty" binding:"required"` // 当前会话ID
	}

	ChatSendResponse struct {
		AiInformation string `json:"Information,omitempty"` // AI回答
		controller.Response
	}

	ChatHistoryRequest struct {
		SessionID string `json:"sessionId,omitempty" binding:"required"` // 当前会话ID
	}

	ChatHistoryResponse struct {
		History []model.History `json:"history"`
		controller.Response
	}
)

// GetUserSessionsByUserName 根据用户名获取用户的所有会话列表
//
// 从JWT中间件中获取用户名，查询并返回该用户的所有会话信息
//
// 成功时返回会话列表，失败时返回服务器繁忙错误
func GetUserSessionsByUserName(c *gin.Context) {
	res := new(GetUserSessionsResponse)

	userName := c.GetString("userName") // From JWT middleware

	userSessions, err := session.GetUserSessionsByUserName(userName)
	if err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeServerBusy))
		return
	}

	res.Success()
	res.Sessions = userSessions

	c.JSON(http.StatusOK, res)
}

// CreateSessionAndSendMessage 处理创建新会话并发送第一条消息的请求
//
// 从JWT中获取用户名，解析请求体中的用户问题和模型类型
// 调用服务层创建会话并获取AI回答
//
// 成功时返回包含AI回答和会话ID的响应
// 失败时返回对应的错误码和消息
func CreateSessionAndSendMessage(c *gin.Context) {
	req := new(CreateSessionAndSendMessageRequest)
	res := new(CreateSessionAndSendMessageResponse)

	userName := c.GetString("userName") // From JWT middleware

	//解析参数
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	if req.ModelType == "medical_agent" {
		medical.Chat(c, req.UserQuestion, "", req.ReportID, false)
		return
	}

	//内部会创建会话并发送消息，并会将AI回答、当前会话返回
	session_id, aiInformation, code_ := session.CreateSessionAndSendMessage(userName, req.UserQuestion, req.ModelType)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.AiInformation = aiInformation
	res.SessionID = session_id

	c.JSON(http.StatusOK, res)
}

// CreateStreamSessionAndSendMessage 处理流式会话创建和消息发送请求
//
// 该函数使用 Server-Sent Events (SSE) 技术实现流式响应
//
// 首先创建会话并立即返回 sessionId 给前端，然后开始流式发送 AI 回答
//
// 参数:
//
//	c: Gin 上下文对象，包含请求信息和响应方法
//
// 注意:
//  1. 需要有效的 JWT 认证中间件提供 userName
//  2. 请求体必须包含有效的 UserQuestion 和 ModelType 参数
//  3. 响应头已配置为 SSE 流式传输所需的标准设置
func CreateStreamSessionAndSendMessage(c *gin.Context) {
	req := new(CreateSessionAndSendMessageRequest)

	userName := c.GetString("userName") // From JWT middleware

	// 解析参数
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, gin.H{"error": "Invalid parameters"})
		return
	}
	if req.ModelType == "medical_agent" {
		medical.Chat(c, req.UserQuestion, "", req.ReportID, true)
		return
	}

	// 设置SSE头
	c.Header("Content-Type", "text/event-stream") // 设置为SSE流式传输
	c.Header("Cache-Control", "no-cache")         // 禁止缓存
	c.Header("Connection", "keep-alive")          // 保持连接
	c.Header("Access-Control-Allow-Origin", "*")  // 允许跨域
	c.Header("X-Accel-Buffering", "no")           // 禁止代理缓存

	// 先创建会话并立即把 sessionId 下发给前端，随后再开始流式输出
	sessionID, code_ := session.CreateStreamSessionOnly(userName, req.UserQuestion, req.ModelType)
	if code_ != code.CodeSuccess {
		c.SSEvent("error", gin.H{"message": "Failed to create session"})
		return
	}
	// 把 sessionId 通过 data 事件发送给前端，前端据此绑定当前会话，侧边栏即可出现新标签
	// SSE 使用特定的文本格式传输数据：
	// 每条消息以 data: 开头
	// 消息内容可以是任意文本，通常是 JSON
	// 每条消息以两个换行符 \n\n 结束
	c.Writer.WriteString(fmt.Sprintf("data: {\"sessionId\": \"%s\"}\n\n", sessionID))
	c.Writer.Flush() // 确保数据立即发送，不被缓冲

	// 然后开始把本次回答进行流式发送（包含最后的 [DONE]）
	code_ = session.StreamMessageToExistingSession(userName, sessionID, req.UserQuestion, req.ModelType, http.ResponseWriter(c.Writer))
	if code_ != code.CodeSuccess {
		// SSE 错误通过特殊事件类型发送
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}
}

// ChatSend 处理在已有会话中发送消息的请求
//
// 从JWT中获取用户名，解析请求体中的会话ID、用户问题和模型类型
// 调用服务层发送消息并获取AI回答
//
// 成功时返回包含AI回答的响应
// 失败时返回对应的错误码和消息
func ChatSend(c *gin.Context) {
	req := new(ChatSendRequest)
	res := new(ChatSendResponse)

	userName := c.GetString("userName") // From JWT middleware

	// 解析参数
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	if req.ModelType == "medical_agent" {
		medical.Chat(c, req.UserQuestion, req.SessionID, req.ReportID, false)
		return
	}

	// 调用服务层，发送消息并获取AI回答
	// log.Println("chatsend req: ", req)
	aiInformation, code_ := session.ChatSend(userName, req.SessionID, req.UserQuestion, req.ModelType)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.AiInformation = aiInformation

	c.JSON(http.StatusOK, res)
}

// ChatStreamSend 处理流式聊天消息发送请求
//
// 该函数使用 Server-Sent Events (SSE) 技术实现流式响应
// 从JWT中获取用户名，解析请求体中的会话ID、用户问题和模型类型
// 调用服务层进行流式消息处理
//
// 参数:
//
//	c: Gin上下文对象，包含请求信息和响应方法
//
// 注意:
//  1. 需要有效的JWT认证中间件提供userName
//  2. 请求体必须包含有效的SessionID、UserQuestion和ModelType参数
//  3. 响应头已配置为SSE流式传输所需的标准设置
//  4. 错误通过SSE的error事件返回
func ChatStreamSend(c *gin.Context) {
	req := new(ChatSendRequest)

	userName := c.GetString("userName") // From JWT middleware

	// 解析参数
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, gin.H{"error": "Invalid parameters"})
		return
	}
	if req.ModelType == "medical_agent" {
		medical.Chat(c, req.UserQuestion, req.SessionID, req.ReportID, true)
		return
	}

	// 设置SSE头
	c.Header("Content-Type", "text/event-stream") // 设置为SSE流式传输
	c.Header("Cache-Control", "no-cache")         // 禁止缓存
	c.Header("Connection", "keep-alive")          // 保持连接
	c.Header("Access-Control-Allow-Origin", "*")  // 允许跨域
	c.Header("X-Accel-Buffering", "no")           // 禁止代理缓存

	// 调用服务层处理流式消息
	code_ := session.ChatStreamSend(userName, req.SessionID, req.UserQuestion, req.ModelType, http.ResponseWriter(c.Writer))
	if code_ != code.CodeSuccess {
		// 通过SSE的error事件发送错误
		c.SSEvent("error", gin.H{"message": "Failed to send message"})
		return
	}
}

// ChatHistory 处理获取聊天历史记录的请求
// 从JWT中间件获取用户名，验证请求参数后查询指定会话ID的聊天记录
// 成功时返回聊天历史记录，失败时返回相应的错误码
func ChatHistory(c *gin.Context) {
	req := new(ChatHistoryRequest)
	res := new(ChatHistoryResponse)

	userName := c.GetString("userName") // From JWT middleware

	// 解析参数
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}

	history, code_ := session.GetChatHistory(userName, req.SessionID)
	if code_ != code.CodeSuccess {
		c.JSON(http.StatusOK, res.CodeOf(code_))
		return
	}

	res.Success()
	res.History = history

	c.JSON(http.StatusOK, res)
}

// SaveSessionModel 保存会话模型选择，无需发送消息。
func SaveSessionModel(c *gin.Context) {
	var req struct {
		SessionID string `json:"sessionId" binding:"required"`
		ModelType string `json:"modelType" binding:"required"`
	}
	res := new(controller.Response)
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidParams))
		return
	}
	c.JSON(http.StatusOK, res.CodeOf(session.SaveSessionModel(c.GetString("userName"), req.SessionID, req.ModelType)))
}
