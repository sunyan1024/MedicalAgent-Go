package router

import (
	"GoAI/controller/session"
	"GoAI/controller/tts"

	"github.com/gin-gonic/gin"
)

// AIRouter 注册AI相关的路由到指定的gin路由组
//
// 包含聊天会话管理、消息发送和历史记录查询等接口
func AIRouter(r *gin.RouterGroup) {
	// 聊天相关接口
	{
		r.GET("/chat/sessions", session.GetUserSessionsByUserName)

		r.POST("/chat/send-new-session", session.CreateSessionAndSendMessage)
		r.POST("/chat/send-stream-new-session", session.CreateStreamSessionAndSendMessage)

		r.POST("/chat/send", session.ChatSend)
		r.POST("/chat/send-stream", session.ChatStreamSend)

		r.POST("/chat/history", session.ChatHistory)

		r.POST("/rag/evaluate", session.RAGEvaluate)

		// TTS相关接口
		r.POST("/chat/tts", tts.CreateTTSTask)
		r.GET("/chat/tts/query", tts.QueryTTSTask)
	}
}
