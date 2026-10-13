package model

import "time"

// UserMemory is the single authoritative Markdown document for a user.
type UserMemory struct {
	UID             string    `gorm:"primaryKey;type:varchar(50)" json:"-"`
	ContentMarkdown string    `gorm:"type:longtext;not null" json:"contentMarkdown"`
	Version         int64     `gorm:"not null" json:"version"`
	CreatedAt       time.Time `json:"-"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
