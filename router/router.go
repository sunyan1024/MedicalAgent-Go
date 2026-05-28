package router

import (
	"GoAI/middleware/jwt"

	"github.com/gin-gonic/gin"
)

// InitRouter 初始化并配置 Gin 路由引擎
// 返回配置好的 *gin.Engine 实例，包含以下路由组：
// - /api/v2/user: 用户相关路由
// - /api/v2/AI: 需要 JWT 认证的 AI 相关路由
// - /api/v2/image: 需要 JWT 认证的图像相关路由
// - /api/v2/file: 需要 JWT 认证的文件相关路由
func InitRouter() *gin.Engine {
	r := gin.Default()

	enterRouter := r.Group("/api/v2")

	{
		RegisterUserRouter(enterRouter.Group("/user"))
	}

	//后续登录的接口需要jwt鉴权
	{
		AIGroup := enterRouter.Group("/AI")
		AIGroup.Use(jwt.Auth())
		AIRouter(AIGroup)
	}

	{
		ImageGroup := enterRouter.Group("/image")
		ImageGroup.Use(jwt.Auth())
		ImageRouter(ImageGroup)
	}

	{
		FileGroup := enterRouter.Group("/file")
		FileGroup.Use(jwt.Auth())
		FileRouter(FileGroup)
	}

	return r
}
