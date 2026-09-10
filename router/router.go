package router

import (
	"GoAI/common/logging"
	"GoAI/middleware/jwt"
	"fmt"
	"os"
	"time"

	"github.com/gin-gonic/gin"
)

// InitRouter 初始化并配置 Gin 路由引擎
// 返回配置好的 *gin.Engine 实例，包含以下路由组：
// - /api/v2/user: 用户相关路由
// - /api/v2/AI: 需要 JWT 认证的 AI 相关路由
// - /api/v2/image: 需要 JWT 认证的图像相关路由
// - /api/v2/file: 需要 JWT 认证的文件相关路由
func InitRouter() *gin.Engine {
	gin.DefaultWriter = logging.NewColoredTimeWriter(os.Stdout)
	gin.DefaultErrorWriter = logging.NewColoredTimeWriter(os.Stderr)

	r := gin.New()
	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		Output: os.Stdout,
		Formatter: func(param gin.LogFormatterParams) string {
			return fmt.Sprintf("%s [GIN] |%s %3d %s| %13v | %15s |%-7s %#v\n%s",
				logging.ColoredDateTime(param.TimeStamp),
				param.StatusCodeColor(),
				param.StatusCode,
				param.ResetColor(),
				param.Latency.Truncate(time.Microsecond),
				param.ClientIP,
				param.MethodColor()+param.Method+param.ResetColor(),
				param.Path,
				param.ErrorMessage,
			)
		},
	}))
	r.Use(gin.RecoveryWithWriter(gin.DefaultErrorWriter))

	enterRouter := r.Group("/api/v2")

	{
		userGroup := enterRouter.Group("/user")
		RegisterUserRouter(userGroup)
	}

	//后续登录的接口需要jwt鉴权
	{
		AIGroup := enterRouter.Group("/AI")
		AIGroup.Use(jwt.Auth(), jwt.RequireUser())
		AIRouter(AIGroup)
	}

	{
		ImageGroup := enterRouter.Group("/image")
		ImageGroup.Use(jwt.Auth(), jwt.RequireUser())
		ImageRouter(ImageGroup)
	}

	{
		FileGroup := enterRouter.Group("/file")
		FileGroup.Use(jwt.Auth(), jwt.RequireUser())
		FileRouter(FileGroup)
	}

	enterprise := enterRouter.Group("/enterprise/file", jwt.Auth())
	EnterpriseFileRouter(enterprise)

	return r
}
