package session

import (
	"GoAI/common/aihelper"
	"GoAI/common/code"
	"GoAI/common/mysql"
	"GoAI/dao/session"
	"GoAI/model"
	"GoAI/utils"
	"context"
	"log"
	"net/http"
	"unicode/utf8"
)

// ctx 全局上下文，用于AI助手管理器的回调函数
var ctx = context.Background()

// GetUserSessionsByUID 根据用户 UID获取用户的所有会话信息
// 参数:
//   - uid: 要查询的用户 UID
//
// 返回:
//   - []model.SessionInfo: 用户会话信息列表，包含会话ID和标题
//   - error: 错误信息，成功时为nil
func GetUserSessionsByUID(uid string) ([]model.SessionInfo, error) {
	//获取用户的所有会话ID
	// manager := aihelper.GetGlobalManager()
	// sessions := manager.GetUserSessions(uid)

	// 从数据库获取会话信息
	sessions, err := session.GetSessionsByUID(uid)
	if err != nil {
		return nil, err
	}

	var SessionInfos []model.SessionInfo

	for _, s := range sessions {
		SessionInfos = append(SessionInfos, model.SessionInfo{
			SessionID: s.ID,
			ModelType: s.ModelType,
			Title:     s.Title,     // 使用数据库中的title
			UpdatedAt: s.UpdatedAt, // 使用数据库中的updated_at
		})
	}

	return SessionInfos, nil
}

// 安全截取UTF-8字符串函数，确保不会截断多字节字符
func safeTruncateString(s string, maxBytes int) string {
	// 如果字符串的字节长度已经小于等于最大值，直接返回
	if len(s) <= maxBytes {
		return s
	}

	// 保留3个字节用于省略号"..."（UTF-8编码下占3个字节）
	truncatedMaxBytes := maxBytes - 3

	// 确保不会在UTF-8字符的中间截断
	// 需要留出至少4个字节的空间（UTF-8最多占用4字节/字符）
	// 从截取点开始往前找，找到一个有效的UTF-8边界
	for i := truncatedMaxBytes; i > truncatedMaxBytes-4 && i > 0; i-- {
		if utf8.ValidString(s[:i]) {
			return s[:i] + "..."
		}
	}

	// 如果找不到合适的截断点，返回一个默认值
	return "新会话"
}

// CreateSessionAndSendMessage 创建一个新的会话并发送用户问题，返回会话ID、AI回复内容和状态码
// 参数:
//
//	uid: 发起请求的用户 UID
//	userQuestion: 用户提出的问题
//	modelType: 使用的AI模型类型
//
// 返回值:
//
//	会话ID, AI回复内容, 状态码
func CreateSessionAndSendMessage(uid string, userQuestion string, modelType string) (string, string, code.Code) {
	//1：创建一个新的会话
	// 安全截取用户问题的前90个字节作为标题
	// 注意：数据库中title字段定义为varchar(100)，所以我们取90个字节确保有足够空间
	title := safeTruncateString(userQuestion, 90)

	//创建会话
	newSession := &model.Session{
		ID:        utils.GenerateUUID(),
		UID:       uid,
		ModelType: modelType,
		Title:     title, // 使用截取后的问题作为标题
	}
	createdSession, err := session.CreateSession(newSession)
	if err != nil {
		log.Println("CreateSessionAndSendMessage CreateSession error:", err)
		return "", "", code.CodeServerBusy
	}

	//2：获取AIHelper并通过其管理消息
	manager := aihelper.GetGlobalManager()
	config := map[string]interface{}{"uid": uid} // 用于 RAG 模型获取用户文档
	helper, err := manager.GetOrCreateAIHelper(uid, createdSession.ID, modelType, config)
	if err != nil {
		log.Println("CreateSessionAndSendMessage GetOrCreateAIHelper error:", err)
		return "", "", code.AIModelFail
	}

	//3：生成AI回复
	aiResponse, err_ := helper.GenerateResponse(uid, ctx, userQuestion)
	if err_ != nil {
		log.Println("CreateSessionAndSendMessage GenerateResponse error:", err_)
		return "", "", code.AIModelFail
	}

	return createdSession.ID, aiResponse.Content, code.CodeSuccess
}

// CreateStreamSessionOnly 创建一个新的流式会话并返回会话ID
// 参数:
//   - uid: 用户 UID
//   - userQuestion: 用户问题，将被截取前90个字节作为会话标题
//
// 返回值:
//   - string: 新创建的会话ID
//   - code.Code: 操作状态码，成功返回code.CodeSuccess，失败返回code.CodeServerBusy
func CreateStreamSessionOnly(uid string, userQuestion string, modelType string) (string, code.Code) {
	// 安全截取用户问题的前90个字节作为标题
	title := safeTruncateString(userQuestion, 90)

	// 创建会话
	newSession := &model.Session{
		ID:        utils.GenerateUUID(),
		UID:       uid,
		ModelType: modelType,
		Title:     title, // 使用截取后的问题作为标题
	}
	createdSession, err := session.CreateSession(newSession)
	if err != nil {
		log.Println("CreateStreamSessionOnly CreateSession error:", err)
		return "", code.CodeServerBusy
	}

	return createdSession.ID, code.CodeSuccess
}

// StreamMessageToExistingSession 向现有会话流式发送消息
//
// 参数:
//
//	uid: 用户 UID，用于标识用户
//	sessionID: 会话ID，标识特定会话
//	userQuestion: 用户提问内容
//	modelType: 使用的AI模型类型
//	writer: HTTP响应写入器，用于流式输出
//
// 返回值:
//
//	code.Code: 操作状态码，成功返回code.CodeSuccess
//
// 功能说明:
//  1. 检查writer是否支持Flush接口
//  2. 获取AI助手管理器并配置模型参数
//  3. 通过回调函数实时发送SSE格式的AI响应
//  4. 最后发送[DONE]标记表示流结束
func StreamMessageToExistingSession(uid string, sessionID string, userQuestion string, modelType string, writer http.ResponseWriter) code.Code {
	if status := SaveSessionModel(uid, sessionID, modelType); status != code.CodeSuccess {
		return status
	}
	// 确保 writer 支持 Flush
	flusher, ok := writer.(http.Flusher)
	if !ok {
		log.Println("[StreamMessageToExistingSession] streaming unsupported")
		return code.CodeServerBusy
	}

	// 获取AI助手管理器并配置模型参数
	manager := aihelper.GetGlobalManager()
	config := map[string]interface{}{"uid": uid} // 用于 RAG 模型获取用户文档
	helper, err := manager.GetOrCreateAIHelper(uid, sessionID, modelType, config)
	if err != nil {
		log.Println("[StreamMessageToExistingSession] GetOrCreateAIHelper error:", err)
		return code.AIModelFail
	}

	// 定义回调函数，用于实时发送SSE格式的AI响应
	cb := func(msg string) {
		// 直接发送数据，不转义
		// SSE 格式：data: <content>\n\n
		log.Printf("[SSE] Sending chunk: %s (len=%d)\n", msg, len(msg))

		_, err := writer.Write([]byte("data: " + msg + "\n\n"))
		if err != nil {
			log.Println("[SSE] Write error: ", err)
			return
		}
		flusher.Flush() // 每次必须 flush
		log.Println("[SSE] Flushed")
	}

	// 调用流式生成函数
	_, err_ := helper.StreamResponse(uid, ctx, cb, userQuestion)
	if err_ != nil {
		log.Println("[StreamMessageToExistingSession] StreamResponse error:", err_)
		return code.AIModelFail
	}

	// 发送[DONE]标记表示流结束
	_, err = writer.Write([]byte("data: [DONE]\n\n"))
	if err != nil {
		log.Println("[StreamMessageToExistingSession] write DONE error:", err)
		return code.AIModelFail
	}
	flusher.Flush() // 每次必须 flush

	return code.CodeSuccess
}

// CreateStreamSessionAndSendMessage 创建流式会话并发送消息 （没用上）
//
// 参数:
//
//	uid: 用户 UID，用于标识会话所属用户
//	userQuestion: 用户提问内容
//	modelType: 使用的AI模型类型
//	writer: HTTP响应写入器，用于流式输出
//
// 返回值:
//
//	string: 创建的会话ID
//	code.Code: 操作状态码，CodeSuccess表示成功
func CreateStreamSessionAndSendMessage(uid string, userQuestion string, modelType string, writer http.ResponseWriter) (string, code.Code) {
	sessionID, code_ := CreateStreamSessionOnly(uid, userQuestion, modelType)
	if code_ != code.CodeSuccess {
		return "", code_
	}

	code_ = StreamMessageToExistingSession(uid, sessionID, userQuestion, modelType, writer)
	if code_ != code.CodeSuccess {

		return sessionID, code_
	}

	return sessionID, code.CodeSuccess
}

// ChatSend 处理用户消息发送请求，获取AI回答
//
// 参数:
//
//	uid: 用户 UID，用于标识用户
//	sessionID: 会话ID，标识当前对话
//	userQuestion: 用户问题内容
//	modelType: 使用的AI模型类型
//
// 返回值:
//
//	string: AI生成的回答内容
//	code.Code: 操作状态码，成功返回CodeSuccess，失败返回对应错误码
func ChatSend(uid string, sessionID string, userQuestion string, modelType string) (string, code.Code) {
	if status := SaveSessionModel(uid, sessionID, modelType); status != code.CodeSuccess {
		return "", status
	}
	//1：获取AIHelper
	manager := aihelper.GetGlobalManager()
	config := map[string]interface{}{"uid": uid} // 用于 RAG 模型获取用户文档
	helper, err := manager.GetOrCreateAIHelper(uid, sessionID, modelType, config)
	if err != nil {
		log.Println("ChatSend GetOrCreateAIHelper error:", err)
		return "", code.AIModelFail
	}

	//2：生成AI回复
	aiResponse, err_ := helper.GenerateResponse(uid, ctx, userQuestion)
	if err_ != nil {
		log.Println("ChatSend GenerateResponse error:", err_)
		return "", code.AIModelFail
	}

	return aiResponse.Content, code.CodeSuccess
}

// ChatStreamSend 处理流式会话消息发送请求
//
// 使用 Server-Sent Events (SSE) 技术向现有会话发送消息并流式返回AI回答
//
// 参数:
//
//	uid: 从JWT获取的用户 UID
//	sessionID: 目标会话ID
//	userQuestion: 用户问题内容
//	modelType: 使用的AI模型类型
//	writer: HTTP响应写入器，用于流式输出
//
// 返回:
//
//	操作状态码，表示成功或失败原因
func ChatStreamSend(uid string, sessionID string, userQuestion string, modelType string, writer http.ResponseWriter) code.Code {
	return StreamMessageToExistingSession(uid, sessionID, userQuestion, modelType, writer)
}

// GetChatHistory 根据用户 UID和会话ID获取聊天历史记录
//
// 参数:
//
//	uid: 用户 UID
//	sessionID: 会话ID
//
// 返回值:
//
//	[]model.History: 聊天历史记录数组
//	code.Code: 操作状态码
//
// 注意:
//  1. 历史记录中的消息会按用户和AI交替的顺序标记
//  2. 如果找不到对应的AI助手，返回服务器繁忙错误
func GetChatHistory(uid string, sessionID string) ([]model.History, code.Code) {
	// 获取AIHelper中的消息历史
	manager := aihelper.GetGlobalManager()
	helper, exists := manager.GetAIHelper(uid, sessionID)
	var medicalCount int64
	if mysql.DB.Migrator().HasTable(&model.MedicalRun{}) {
		if err := mysql.DB.Model(&model.MedicalRun{}).Where("uid = ? AND session_id = ?", uid, sessionID).Count(&medicalCount).Error; err != nil {
			return nil, code.CodeServerBusy
		}
	}
	if !exists || medicalCount > 0 {
		var records []model.Message
		if err := mysql.DB.Where("uid = ? AND session_id = ?", uid, sessionID).Order("id asc").Find(&records).Error; err != nil {
			return nil, code.CodeServerBusy
		}
		history := make([]model.History, 0, len(records))
		for _, msg := range records {
			history = append(history, model.History{IsUser: msg.IsUser, Content: msg.Content})
		}
		return history, code.CodeSuccess
	}

	messages := helper.GetMessages()
	history := make([]model.History, 0, len(messages))

	// 转换model.Message为model.History（直接使用消息的IsUser字段）
	for _, msg := range messages {
		history = append(history, model.History{
			IsUser:  msg.IsUser,
			Content: msg.Content,
		})
	}

	return history, code.CodeSuccess
}

// SaveSessionModel 保存当前用户会话的模型选择。
func SaveSessionModel(uid, sessionID, modelType string) code.Code {
	switch modelType {
	case "ollama", "qwen", "rag", "mcp", "medical_agent":
	default:
		return code.CodeInvalidParams
	}
	// 先查询归属，避免 MySQL 对未变化的更新返回零行时误判。
	existing, err := session.GetSessionByID(sessionID)
	if err != nil || existing.UID != uid {
		return code.CodeRecordNotFound
	}
	if err := mysql.DB.Model(&model.Session{}).Where("id = ? AND uid = ?", sessionID, uid).Update("model_type", modelType).Error; err != nil {
		return code.CodeServerBusy
	}
	return code.CodeSuccess
}
