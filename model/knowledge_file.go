package model

import "time"

// KnowledgeFile 记录用户知识库中的单个文件。
type KnowledgeFile struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserName     string    `gorm:"index;not null" json:"username"`
	OriginalName string    `gorm:"type:varchar(255);not null" json:"original_name"`
	StoredName   string    `gorm:"type:varchar(255);uniqueIndex;not null" json:"stored_name"`
	FilePath     string    `gorm:"type:varchar(500);not null" json:"file_path"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
