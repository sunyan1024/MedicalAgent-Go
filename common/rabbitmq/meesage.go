package rabbitmq

import (
	"GoAI/dao/message"
	"GoAI/dao/session"
	"GoAI/model"
	"encoding/json"

	"github.com/streadway/amqp"
)

// 用于消息传输的数据结构
type MessageMQParam struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	UID       string `json:"uid"`
	IsUser    bool   `json:"is_user"`
}

// 将消息参数序列化为 JSON 字节数组
func GenerateMessageMQParam(sessionID string, content string, uid string, IsUser bool) []byte {
	param := MessageMQParam{
		SessionID: sessionID,
		Content:   content,
		UID:       uid,
		IsUser:    IsUser,
	}
	data, _ := json.Marshal(param) // 将 MessageMQParam 结构体 序列化为 JSON 字节数组
	return data
}

// 消息消费处理函数
// 将 JSON 消息体反序列化为 MessageMQParam 结构体
func MQMessage(msg *amqp.Delivery) error {
	var param MessageMQParam
	err := json.Unmarshal(msg.Body, &param) // 将 JSON 消息体 反序列化为 MessageMQParam 结构体
	if err != nil {
		return err
	}
	// 构建 model.Message 对象用于数据库存储
	newMsg := &model.Message{
		SessionID: param.SessionID,
		Content:   param.Content,
		UID:       param.UID,
		IsUser:    param.IsUser,
	}
	//消费者异步插入到数据库中
	_, err = message.CreateMessage(newMsg)
	if err != nil {
		return err
	}

	// 更新对应 session 的 updated_at 字段
	err = session.UpdateSessionTimestamp(param.SessionID)
	if err != nil {
		return err
	}

	return nil
}
