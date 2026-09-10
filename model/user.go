package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	RoleUser  = "user"
	RoleAdmin = "admin"
)

// User 表示系统中的用户模型
type User struct {
	Role      string         `gorm:"type:varchar(16);not null;default:user" json:"role"`
	ID        int64          `gorm:"primaryKey" json:"id"`                         // 用户主键
	Name      string         `gorm:"type:varchar(50)" json:"name"`                 // 用户昵称
	Email     string         `gorm:"type:varchar(100);index" json:"email"`         // 用户邮箱，建立索引
	Username  string         `gorm:"type:varchar(50);uniqueIndex" json:"username"` // 唯一用户名，建立唯一索引
	Password  string         `gorm:"type:varchar(255)" json:"-"`                   // 加密后的密码，不返回给前端
	CreatedAt time.Time      `json:"created_at"`                                   // 记录创建时间，自动时间戳
	UpdatedAt time.Time      `json:"updated_at"`                                   // 记录更新时间，自动时间戳
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`                               // 软删除标记，不返回给前端
}
