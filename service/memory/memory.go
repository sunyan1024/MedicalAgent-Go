package memory

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"GoAI/model"
	driver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrConflict    = errors.New("记忆已被其他会话修改，请重新读取并合并后保存")
	ErrInvalid     = errors.New("记忆最多8000个字符，版本必须为非负整数")
	ErrUnavailable = errors.New("记忆服务暂不可用，请稍后重试")
)

type Store struct{ db *gorm.DB }

func New(db *gorm.DB) *Store {
	// Never interpolate personal memory into SQL logs, including failed writes.
	return &Store{db: db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})}
}

func (s *Store) Read(ctx context.Context, uid string) (model.UserMemory, error) {
	var doc model.UserMemory
	if strings.TrimSpace(uid) == "" {
		return doc, ErrUnavailable
	}
	err := s.db.WithContext(ctx).Where("uid = ?", uid).First(&doc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.UserMemory{UID: uid}, nil
	}
	if err != nil {
		return model.UserMemory{}, ErrUnavailable
	}
	return doc, nil
}

func (s *Store) Update(ctx context.Context, uid, content string, expected int64) (model.UserMemory, error) {
	if !utf8.ValidString(content) || utf8.RuneCountInString(content) > 8000 || expected < 0 || expected == 1<<63-1 {
		return model.UserMemory{}, ErrInvalid
	}
	if strings.TrimSpace(uid) == "" {
		return model.UserMemory{}, ErrUnavailable
	}
	doc := model.UserMemory{UID: uid, ContentMarkdown: content, Version: expected + 1, UpdatedAt: time.Now().UTC()}
	if expected == 0 {
		err := s.db.WithContext(ctx).Create(&doc).Error
		var duplicate *driver.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return model.UserMemory{}, ErrConflict
		}
		if err != nil {
			return model.UserMemory{}, ErrUnavailable
		}
		return doc, nil
	}
	result := s.db.WithContext(ctx).Model(&model.UserMemory{}).Where("uid = ? AND version = ?", uid, expected).
		Updates(map[string]any{"content_markdown": content, "version": doc.Version, "updated_at": doc.UpdatedAt})
	if result.Error != nil {
		return model.UserMemory{}, ErrUnavailable
	}
	if result.RowsAffected != 1 {
		return model.UserMemory{}, ErrConflict
	}
	return doc, nil
}
