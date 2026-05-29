package jwt

import (
	"GoAI/common/code"
	"GoAI/controller"
	"GoAI/utils/myjwt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Auth 返回一个 Gin 中间件函数，用于验证请求的 JWT Token
//
// 从 Authorization 头部或 URL 查询参数中获取 Token，验证其有效性
// 验证成功后将用户名存入 Gin 上下文供后续处理使用
//
// 验证失败时返回无效 Token 错误并终止请求处理
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		res := new(controller.Response)

		// 从 Authorization 头部或 URL 查询参数中获取 Token
		var token string
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
			// Bearer 格式
			token = strings.TrimPrefix(authHeader, "Bearer ")
		} else {
			// 兼容 URL 参数传 token
			token = c.Query("token")
		}

		// 如果没有token，则返回无效token错误，并终止请求处理
		if token == "" {
			log.Printf("[Auth] missing token ip=%s path=%s", c.ClientIP(), c.Request.URL.Path)
			c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		log.Println("[Auth] token is", maskToken(token))

		// 解析并验证token
		userName, ok := myjwt.ParseToken(token)
		if !ok {
			log.Printf("[Auth] invalid token ip=%s path=%s", c.ClientIP(), c.Request.URL.Path)
			c.JSON(http.StatusOK, res.CodeOf(code.CodeInvalidToken))
			c.Abort()
			return
		}

		// 验证成功，将用户名存入Gin上下文
		c.Set("userName", userName)
		c.Next()
	}
}

func maskToken(token string) string {
	const visibleChars = 10
	if len(token) <= visibleChars*2 {
		return "<redacted>"
	}

	return token[:visibleChars] + "..." + token[len(token)-visibleChars:]
}
