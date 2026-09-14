package model

import (
	"time"

	"gorm.io/gorm"
)

// Session 表示用户会话数据结构，包含会话的基本信息和时间标记
type Session struct {
	ModelType string         `gorm:"type:varchar(32);not null;default:medical_agent" json:"modelType"`
	ID        string         `gorm:"primaryKey;type:varchar(36)" json:"id"` // 会话的唯一标识符，使用UUID格式
	UID       string         `gorm:"index;not null" json:"uid"`             // 关联的用户 UID，建立索引且不能为空
	Title     string         `gorm:"type:varchar(100)" json:"title"`        // 会话的标题，最大长度100个字符
	CreatedAt time.Time      `json:"created_at"`                            // 记录会话创建时间
	UpdatedAt time.Time      `json:"updated_at"`                            // 记录会话最后更新时间
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`                        // 用于软删除标记，不包含在JSON输出中
}

// SessionInfo 表示会话的基本信息
type SessionInfo struct {
	ModelType string    `json:"modelType"`
	SessionID string    `json:"sessionId"`  // 会话的唯一标识符
	Title     string    `json:"title"`      // 会话的标题
	UpdatedAt time.Time `json:"updated_at"` // 会话的更新时间
}
