package model

import "time"

// KnowledgeFile 记录用户知识库中的单个文件。
type KnowledgeFile struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserName     string    `gorm:"index;uniqueIndex:idx_user_stored_name,priority:1;not null" json:"username"`
	OriginalName string    `gorm:"type:varchar(255);not null" json:"original_name"`
	StoredName   string    `gorm:"type:varchar(255);uniqueIndex:idx_user_stored_name,priority:2;not null" json:"stored_name"`
	StoredFileID string    `gorm:"type:varchar(36);index;not null" json:"stored_file_id"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StoredFile 记录全局物理文件。用户知识库文件通过 StoredFileID 引用它。
type StoredFile struct {
	ID         string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	FileSHA256 string    `gorm:"type:char(64);uniqueIndex:idx_sha256_ext,priority:1;not null" json:"file_sha256"`
	Ext        string    `gorm:"type:varchar(16);uniqueIndex:idx_sha256_ext,priority:2;not null" json:"ext"`
	StoredName string    `gorm:"type:varchar(255);not null" json:"stored_name"`
	FilePath   string    `gorm:"type:varchar(500);not null" json:"file_path"`
	Size       int64     `json:"size"`
	RefCount   int       `gorm:"not null;default:0" json:"ref_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
