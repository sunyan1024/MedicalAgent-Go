package user

import (
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"GoAI/config"
	"GoAI/model"
	"GoAI/utils"
	"gorm.io/gorm"
)

type adminCredentials struct{ username, email, password string }

func readAdminCredentials(cfg config.AdminConfig) (adminCredentials, error) {
	c := adminCredentials{strings.TrimSpace(cfg.Username), strings.TrimSpace(cfg.Email), cfg.Password}
	if c.username == "" && c.email == "" && c.password == "" {
		return c, nil
	}
	if c.username == "" || c.email == "" || strings.TrimSpace(c.password) == "" {
		return c, fmt.Errorf("管理员初始化需要同时设置 config/config.toml 中 adminConfig 的 username、email、password")
	}
	address, err := mail.ParseAddress(c.email)
	if err != nil || address.Address != c.email || utf8.RuneCountInString(c.email) > 100 || utf8.RuneCountInString(c.username) > 50 || len(c.password) > 72 {
		return c, fmt.Errorf("管理员初始化配置无效：检查用户名、邮箱长度和格式，密码不得超过 72 字节")
	}
	return c, nil
}

// BootstrapAdmin never resets a password or elevates an existing ordinary account.
func BootstrapAdmin(db *gorm.DB) error {
	credentials, err := readAdminCredentials(config.GetConfig().AdminConfig)
	if err != nil {
		return err
	}
	return bootstrapAdmin(db, credentials)
}

func bootstrapAdmin(db *gorm.DB, c adminCredentials) error {
	if c.username == "" {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var accounts []model.User
		// Cross-check both login identifiers, including soft-deleted accounts.
		if err := tx.Unscoped().Where("username IN ? OR email IN ?", []string{c.username, c.email}, []string{c.username, c.email}).Find(&accounts).Error; err != nil {
			return err
		}
		if len(accounts) > 0 {
			if len(accounts) == 1 && accounts[0].Username == c.username && accounts[0].Email == c.email && accounts[0].Role == model.RoleAdmin && !accounts[0].DeletedAt.Valid {
				return nil
			}
			return fmt.Errorf("管理员初始化账号冲突：用户名或邮箱已被占用，不会自动提升或覆盖账号")
		}
		hash, err := utils.GenerateFromPassword(c.password)
		if err != nil {
			return err
		}
		return tx.Create(&model.User{Username: c.username, Name: c.username, Email: c.email, Password: string(hash), Role: model.RoleAdmin}).Error
	})
}
