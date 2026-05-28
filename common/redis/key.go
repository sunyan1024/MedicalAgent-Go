package redis

import (
	"GoAI/config"
	"fmt"
)

// GenerateCaptcha 根据邮箱地址生成验证码captcha在 Redis 的存储键key（前缀+邮箱）
//
// 参数 email 为用户的邮箱地址
//
// 返回值为格式化后的 Redis 键字符串，包含配置中的前缀和邮箱
//
// key 特定邮箱-> 验证码
//
//	captcha:<EMAIL> -> captcha
func GenerateCaptcha(email string) string {
	return fmt.Sprintf(config.DefaultRedisKeyConfig.CaptchaPrefix, email) // 前缀+邮箱 captcha:%s
}

// GenerateIndexName 根据给定的文件名生成Redis索引名称，使用配置中定义的索引名称格式
func GenerateIndexName(filename string) string {
	indexName := fmt.Sprintf(config.DefaultRedisKeyConfig.IndexName, filename)
	return indexName
}

// GenerateIndexNamePrefix 根据文件名生成Redis索引名称前缀
// 使用config.DefaultRedisKeyConfig.IndexNamePrefix作为格式模板
func GenerateIndexNamePrefix(filename string) string {
	prefix := fmt.Sprintf(config.DefaultRedisKeyConfig.IndexNamePrefix, filename)
	return prefix
}
