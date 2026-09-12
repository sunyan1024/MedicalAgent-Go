package model

import "time"

// MedicalReport is private task data, never a shared knowledge document.
type MedicalReport struct {
	ID         string    `gorm:"primaryKey;type:varchar(36)" json:"id"`
	UserName   string    `gorm:"index;not null" json:"-"`
	SessionID  string    `gorm:"type:varchar(36);index" json:"sessionId"`
	Name       string    `json:"name"`
	Status     string    `gorm:"type:varchar(24);index" json:"status"`
	Text       string    `gorm:"type:mediumtext" json:"text"`
	Indicators string    `gorm:"type:mediumtext" json:"indicatorsJson"`
	Error      string    `gorm:"type:text" json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type MedicalRun struct {
	ID        string    `gorm:"primaryKey;type:varchar(36)" json:"runId"`
	UserName  string    `gorm:"index;not null" json:"-"`
	SessionID string    `gorm:"type:varchar(36);index" json:"sessionId"`
	ReportID  string    `gorm:"type:varchar(36)" json:"reportId,omitempty"`
	Status    string    `gorm:"type:varchar(24)" json:"status"`
	Question  string    `gorm:"type:text" json:"question"`
	Answer    string    `gorm:"type:mediumtext" json:"answer"`
	Events    string    `gorm:"type:mediumtext" json:"eventsJson"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
