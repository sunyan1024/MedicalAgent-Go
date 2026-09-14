package model

import "time"

// MedicalReport is private task data, never a shared knowledge document.
type MedicalReport struct {
	ID         string    `gorm:"column:report_id;primaryKey;type:varchar(36)" json:"id"`
	UID        string    `gorm:"index;not null" json:"-"`
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
	ID        string    `gorm:"column:run_id;primaryKey;type:varchar(36)" json:"runId"`
	UID       string    `gorm:"index;not null" json:"-"`
	SessionID string    `gorm:"type:varchar(36);index" json:"sessionId"`
	ReportID  string    `gorm:"type:varchar(36)" json:"reportId,omitempty"`
	Status    string    `gorm:"type:varchar(24)" json:"status"`
	Question  string    `gorm:"type:text" json:"question"`
	Answer    string    `gorm:"type:mediumtext" json:"answer"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MedicalRunEvent is an independently committed execution event.
type MedicalRunEvent struct {
	ID         string    `gorm:"column:event_id;primaryKey;type:varchar(36)"`
	RunID      string    `gorm:"type:varchar(36);not null;uniqueIndex:idx_medical_event_sequence,priority:1"`
	Sequence   int64     `gorm:"not null;uniqueIndex:idx_medical_event_sequence,priority:2"`
	ParentID   string    `gorm:"type:varchar(36);index"`
	Type       string    `gorm:"type:varchar(40);not null"`
	Agent      string    `gorm:"type:varchar(100);index:idx_medical_event_agent_time,priority:1"`
	Tool       string    `gorm:"type:varchar(100);index:idx_medical_event_tool_time,priority:1"`
	Status     string    `gorm:"type:varchar(24);index:idx_medical_event_status_time,priority:1"`
	OccurredAt time.Time `gorm:"type:datetime(6);not null;index:idx_medical_event_agent_time,priority:2;index:idx_medical_event_tool_time,priority:2;index:idx_medical_event_status_time,priority:2"`
	DurationMS int64
	Summary    string `gorm:"type:text"`
	Input      string `gorm:"type:longtext"`
	Output     string `gorm:"type:longtext"`
	Error      string `gorm:"type:longtext"`
	Sources    string `gorm:"type:longtext"`
	Delta      string `gorm:"type:longtext"`
}
