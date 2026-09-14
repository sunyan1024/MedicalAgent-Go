package model

import (
	"time"
)

// Message 表示聊天消息的数据结构
type Message struct {
	ID        uint      `gorm:"column:message_id;primaryKey;autoIncrement" json:"id"` // 消息的唯一标识符，自动递增
	SessionID string    `gorm:"index;not null;type:varchar(36)" json:"session_id"`    // 会话的唯一标识符，不能为空
	UID       string    `gorm:"type:varchar(20)" json:"uid"`                          // 发送消息的用户 UID
	Content   string    `gorm:"type:text" json:"content"`                             // 消息的文本内容
	IsUser    bool      `gorm:"not null;" json:"is_user"`                             // 标识消息是否来自用户
	CreatedAt time.Time `json:"created_at"`                                           // 消息的创建时间
}

// History 表示对话历史记录的结构体
type History struct {
	IsUser  bool   `json:"is_user"` // 标识消息是否来自用户
	Content string `json:"content"` // 存储消息内容
}
