package aihelper

import (
	"GoAI/common/rabbitmq"
	"GoAI/model"
	"GoAI/utils"
	"context"
	"fmt"
	"sync"
	"time"
)

// AIHelper AI助手结构体，包含消息历史和AI模型
type AIHelper struct {
	model    AIModel          // AI模型接口，支持OpenAI、Ollama
	messages []*model.Message // 消息历史列表，存储用户和AI助手的对话记录
	mu       sync.RWMutex     // 读写锁，保护消息历史并发访问
	// 一个会话绑定一个AIHelper
	SessionID string                                       // 会话唯一标识，用于绑定消息和上下文
	saveFunc  func(*model.Message) (*model.Message, error) // 消息存储回调函数，默认异步发布到RabbitMQ
}

// NewAIHelper 创建新的AIHelper实例
func NewAIHelper(model_ AIModel, SessionID string) *AIHelper {
	return &AIHelper{
		model:    model_,
		messages: make([]*model.Message, 0),
		//异步推送到消息队列中
		saveFunc: func(msg *model.Message) (*model.Message, error) {
			// 将消息参数序列化为 JSON 字节数组
			data := rabbitmq.GenerateMessageMQParam(msg.SessionID, msg.Content, msg.UID, msg.IsUser, msg.DurationMS)
			// 异步推送到消息队列中
			err := rabbitmq.RMQMessage.Publish(data)
			return msg, err
		},
		SessionID: SessionID,
	}
}

// addMessage 添加消息到内存中并调用自定义存储函数
func (a *AIHelper) AddMessage(Content string, UID string, IsUser bool, Save bool) {
	userMsg := model.Message{
		SessionID: a.SessionID,
		Content:   Content,
		UID:       UID,
		IsUser:    IsUser,
	}
	a.AppendMessage(&userMsg, Save)
}

// AppendMessage 保留完整消息元数据，供生成结果保存和启动时恢复使用。
func (a *AIHelper) AppendMessage(msg *model.Message, save bool) error {
	a.mu.Lock()
	a.messages = append(a.messages, msg) // 内存中维护消息历史
	a.mu.Unlock()

	if save {
		_, err := a.saveFunc(msg) // 异步存储到消息队列
		return err
	}
	return nil
}

// SetSaveFunc 设置消息保存函数，用于自定义消息持久化逻辑
//
// saveFunc: 接收消息对象并返回保存后的消息及错误信息的函数
func (a *AIHelper) SetSaveFunc(saveFunc func(*model.Message) (*model.Message, error)) {
	a.saveFunc = saveFunc
}

// GetMessages 获取所有消息历史
func (a *AIHelper) GetMessages() []*model.Message {
	a.mu.RLock()
	defer a.mu.RUnlock()

	out := make([]*model.Message, len(a.messages))
	copy(out, a.messages)

	return out
}

// 同步生成
func (a *AIHelper) GenerateResponse(uid string, ctx context.Context, userQuestion string) (*model.Message, error) {
	//调用存储函数
	// 这里的userQuestion是用户输入的消息，需要保存到数据库中
	a.AddMessage(userQuestion, uid, true, true)

	a.mu.RLock()
	//将model.Message转化成schema.Message
	messages := utils.ConvertToSchemaMessages(a.messages) // 转化所有消息
	a.mu.RUnlock()

	//调用模型生成回复
	startedAt := time.Now()
	schemaMsg, err := a.model.GenerateResponse(ctx, messages)
	durationMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		return nil, err
	}

	//将schema.Message转化成model.Message
	modelMsg := utils.ConvertToModelMessage(a.SessionID, uid, schemaMsg)

	//添加到内存，并调用存储函数
	modelMsg.DurationMS = durationMS
	a.AppendMessage(modelMsg, true)

	return modelMsg, nil
}

// 流式生成
func (a *AIHelper) StreamResponse(uid string, ctx context.Context, cb StreamCallback, userQuestion string) (*model.Message, error) {
	//调用存储函数
	// 这里的userQuestion是用户输入的消息，需要保存到数据库中
	a.AddMessage(userQuestion, uid, true, true)

	a.mu.RLock()
	// 将model.Message转化成schema.Message
	messages := utils.ConvertToSchemaMessages(a.messages) // 转化所有消息
	a.mu.RUnlock()

	// 调用模型生成回复
	startedAt := time.Now()
	content, err := a.model.StreamResponse(ctx, messages, cb)
	durationMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		return nil, err
	}

	//转化成model.Message
	modelMsg := &model.Message{
		SessionID: a.SessionID,
		UID:       uid,
		Content:   content,
		IsUser:    false,
	}

	// 添加到内存，并调用存储函数
	// 这里的modelMsg是模型生成的消息，需要保存到数据库中
	modelMsg.DurationMS = durationMS
	a.AppendMessage(modelMsg, true)

	return modelMsg, nil
}

// GetModelType 获取模型类型
func (a *AIHelper) GetModelType() string {
	return a.model.GetModelType()
}

// ChangeModelType 更改AIHelper的模型类型
// 参数 newModel: 新的AI模型实例
// 返回值: 成功返回nil，失败返回错误
func (a *AIHelper) ChangeModelType(newModel AIModel) error {
	if newModel == nil {
		return fmt.Errorf("new model cannot be nil")
	}
	a.model = newModel
	return nil
}
