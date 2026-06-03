package utils

import (
	"GoAI/model"
	"fmt"
	"math/rand"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// GetRandomNumbers 生成指定长度的随机数字字符串
// 参数 num 指定要生成的数字个数
// 返回由随机数字组成的字符串
func GetRandomNumbers(num int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	code := ""
	for i := 0; i < num; i++ {
		// 0~9随机数
		digit := r.Intn(10)
		code += strconv.Itoa(digit)
	}
	return code
}

// GenerateFromPassword bcrypt 生成加密后的密码
func GenerateFromPassword(password string) ([]byte, error) {
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

// CompareHashAndPassword  bcrypt 比较加密后的密码和明文密码是否匹配
func CompareHashAndPassword(hashedPassword, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

// GenerateUUID 生成并返回一个新的UUID字符串
func GenerateUUID() string {
	return uuid.New().String()
}

// 将 schema 消息转换为数据库可存储的格式
func ConvertToModelMessage(sessionID string, userName string, msg *schema.Message) *model.Message {
	return &model.Message{
		SessionID: sessionID,
		UserName:  userName,
		Content:   msg.Content,
	}
}

// 将数据库消息转换为 schema 消息（供 AI 使用）
func ConvertToSchemaMessages(msgs []*model.Message) []*schema.Message {
	schemaMsgs := make([]*schema.Message, 0, len(msgs))
	for _, m := range msgs {
		role := schema.Assistant // 默认设置为AI助手
		if m.IsUser {            // 如果m.IsUser为true
			role = schema.User // 则设置为用户
		}
		schemaMsgs = append(schemaMsgs, &schema.Message{
			Role:    role,
			Content: m.Content,
		})
	}
	return schemaMsgs
}

// RemoveAllFilesInDir 删除目录中的所有文件（不删除子目录）
func RemoveAllFilesInDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录不存在就算了
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			filePath := filepath.Join(dir, entry.Name())
			if err := os.Remove(filePath); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateFile 校验文件是否为允许上传的知识库文件（.md、.txt、.pdf）
func ValidateFile(file *multipart.FileHeader) error {
	// 校验文件扩展名
	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".md" && ext != ".txt" && ext != ".pdf" {
		return fmt.Errorf("文件类型不正确，只允许 .md、.txt 或 .pdf 文件，当前扩展名: %s", ext)
	}

	return nil
}
