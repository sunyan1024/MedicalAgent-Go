package myjwt

import (
	"GoAI/config"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

// Claims 结构体定义JWT的声明
type Claims struct {
	UID                  string `json:"uid"` // 用户 UID
	jwt.RegisteredClaims        // 包括过期时间 (exp)、发行者 (iss)、主题 (sub)、发行时间 (iat)
}

// GenerateToken 生成JWT token
// 参数:
//   - uid: 用户 UID
//
// 返回值:
//   - token: JWT token字符串
//   - err: 错误信息
func GenerateToken(uid string) (string, error) {
	claims := Claims{
		UID: uid,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(config.GetConfig().ExpireDuration) * time.Hour)),
			Issuer:    config.GetConfig().JwtConfig.Issuer,
			Subject:   config.GetConfig().JwtConfig.Subject,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	// 生成 token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims) // 签名算法HS256
	signedToken, err := token.SignedString([]byte(config.GetConfig().JwtConfig.SecretKey))
	return signedToken, err
}

// ParseToken 解析JWT token并验证有效性
//
// 参数:
//   - token: JWT token字符串
//
// 返回值:
//   - 解析出的用户 UID
//   - 是否错误
func ParseToken(tokenStr string) (string, bool) {
	// 初始化claims
	claims := new(Claims)

	// 解析token
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		// 验证签名算法
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(config.GetConfig().JwtConfig.SecretKey), nil
	})
	if err != nil || !token.Valid || claims.UID == "" {
		return "", false
	}

	return claims.UID, true
}
