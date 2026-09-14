package session

import (
	"GoAI/common/mysql"
	"GoAI/model"
)

// 根据用户 UID查询所有相关会话
// func GetSessionsByUID(UID int64) ([]model.Session, error) {
func GetSessionsByUID(UID string) ([]model.Session, error) {
	var sessions []model.Session
	// 简单的查询用where
	err := mysql.DB.Where("uid = ?", UID).Find(&sessions).Error
	return sessions, err
}

// 创建新的会话记录
func CreateSession(session *model.Session) (*model.Session, error) {
	err := mysql.DB.Create(session).Error
	return session, err
}

// 根据会话ID查询单个会话
func GetSessionByID(sessionID string) (*model.Session, error) {
	var session model.Session
	err := mysql.DB.Where("session_id = ?", sessionID).First(&session).Error
	return &session, err
}

// 更新会话的updated_at字段
func UpdateSessionTimestamp(sessionID string) error {
	// 更新操作和复杂查询用model
	return mysql.DB.Model(&model.Session{}).Where("session_id = ?", sessionID).Update("updated_at", mysql.DB.NowFunc()).Error
}
