package router

import (
	"GoAI/controller/user"

	"github.com/gin-gonic/gin"
)

// RegisterUserRouter 注册用户相关的路由到指定的路由组
//
// 包括注册、登录和验证码获取接口
//
// 参数 r 是要注册路由的 gin.RouterGroup 实例
func RegisterUserRouter(r *gin.RouterGroup) {
	// Gin 路由处理函数需要符合 gin.HandlerFunc 类型
	{
		r.POST("/register", user.Register) // func Register(c *gin.Context)
		r.POST("/login", user.Login)
		r.POST("/captcha", user.HandleCaptcha)
	}
}
