package model

import (
	"time"

	"gorm.io/gorm"
)

const (
	KnowledgeScopePersonal   = "personal"
	KnowledgeScopeEnterprise = "enterprise"
	EnterpriseOwnerKey       = "default"

	StoredFileIndexStatusPending = "pending"
	StoredFileIndexStatusReady   = "ready"
	StoredFileIndexStatusFailed  = "failed"
)

// KnowledgeFile 记录用户知识库中的单个文件。
type KnowledgeFile struct {
	ID           string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	Scope        string    `gorm:"type:varchar(16);not null;default:personal;uniqueIndex:idx_scope_owner_stored,priority:1" json:"scope"`
	OwnerKey     string    `gorm:"type:varchar(50);not null;default:'';uniqueIndex:idx_scope_owner_stored,priority:2" json:"-"`
	UID          string    `gorm:"index;not null" json:"uid"`
	OriginalName string    `gorm:"type:varchar(255);not null" json:"original_name"`
	StoredName   string    `gorm:"type:varchar(255);uniqueIndex:idx_scope_owner_stored,priority:3;not null" json:"stored_name"`
	StoredFileID string    `gorm:"type:varchar(36);index;not null" json:"stored_file_id"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StoredFile 记录全局物理文件。用户知识库文件通过 StoredFileID 引用它。
type StoredFile struct {
	ID          string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	FileSHA256  string         `gorm:"type:char(64);uniqueIndex:idx_sha256_ext,priority:1;not null" json:"file_sha256"`
	Ext         string         `gorm:"type:varchar(16);uniqueIndex:idx_sha256_ext,priority:2;not null" json:"ext"`
	StoredName  string         `gorm:"type:varchar(255);not null" json:"stored_name"`
	FilePath    string         `gorm:"type:varchar(500);not null" json:"file_path"`
	Size        int64          `json:"size"`
	RefCount    int            `gorm:"not null;default:0" json:"ref_count"`
	IndexStatus string         `gorm:"type:varchar(16);not null;default:'pending';index" json:"index_status"`
	IndexError  string         `gorm:"type:text" json:"index_error,omitempty"`
	IndexedAt   *time.Time     `json:"indexed_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// KnowledgeOwner is constructed by the server, never bound from request data.
type KnowledgeOwner struct {
	Scope string
	Key   string
	UID   string
}

func PersonalKnowledgeOwner(uid string) KnowledgeOwner {
	return KnowledgeOwner{Scope: KnowledgeScopePersonal, Key: uid, UID: uid}
}

func EnterpriseKnowledgeOwner(uid string) KnowledgeOwner {
	return KnowledgeOwner{Scope: KnowledgeScopeEnterprise, Key: EnterpriseOwnerKey, UID: uid}
}
