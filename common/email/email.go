package email

import (
	"GoAI/config"
	"log"

	"gopkg.in/gomail.v2"
)

const (
	CodeMsg = "GoAI验证码如下（验证码仅限于5分钟有效）："
	UIDMsg  = "GoAI的账号如下，请保留好，后续可以用账号/邮箱登录："
)

// 发送验证码或账号
func SendCaptcha(email, code, msg string) error {
	m := gomail.NewMessage()

	// 发件人
	m.SetHeader("From", config.GetConfig().EmailConfig.Email)
	// 收件人
	m.SetHeader("To", email)
	// 主题
	m.SetHeader("Subject", "来自GoAI的信息")
	// 正文内容（纯文本形式，也可以用 text/html）
	m.SetBody("text/plain", msg+" "+code) // 消息前缀 + 验证码/账号

	// 配置 SMTP 服务器和授权码,587：是 SMTP 的明文/STARTTLS 端口号
	d := gomail.NewDialer("smtp.qq.com", 587, config.GetConfig().EmailConfig.Email, config.GetConfig().EmailConfig.Authcode)

	// 发送邮件
	if err := d.DialAndSend(m); err != nil {
		log.Printf("DialAndSend err %v:\n", err)
		return err
	}
	log.Println("send mail success")
	return nil
}
