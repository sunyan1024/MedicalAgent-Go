package redis

import (
	"GoAI/config"
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

func isRediSearchUnsupported(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()
	return strings.Contains(msg, "unknown command `FT.INFO`") ||
		strings.Contains(msg, "unknown command `FT.CREATE`") ||
		strings.Contains(msg, "unknown command `FT.DROPINDEX`")
}

// 全局变量
var (
	Rdb          *redis.Client // 全局 Redis 客户端实例，采用单例模式，整个项目共享同一个连接
	redisOnce    sync.Once
	redisInitErr error
	ctx          = context.Background() //使用 context.Background() 作为默认上下文，这是 Go 处理请求的标准做法
)

// Init 初始化Redis客户端连接，从配置中读取Redis主机地址、端口、密码和数据库编号，
// 并创建新的Redis客户端实例。该函数应在程序启动时调用一次。
func Init() error {
	redisOnce.Do(func() {
		// 获取配置
		conf := config.GetConfig()
		host := conf.RedisConfig.RedisHost
		port := conf.RedisConfig.RedisPort
		password := conf.RedisConfig.RedisPassword
		db := conf.RedisConfig.RedisDb

		// 组装连接地址
		addr := host + ":" + strconv.Itoa(port)

		// 创建客户端
		Rdb = redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
			Protocol: 2, // 使用 Protocol 2 避免 maint_notifications 警告
		})
		// 检查连接
		if err := Rdb.Ping(ctx).Err(); err != nil {
			redisInitErr = err
			Rdb = nil
		}
	})
	return redisInitErr
}

// SetCaptchaForEmail 将验证码与邮箱关联并存储到Redis中，设置5分钟过期时间
//
// 参数：
//
//	email: 用户邮箱地址
//	captcha: 需要存储的验证码
//
// 返回:
//
//	存储操作可能发生的错误
func SetCaptchaForEmail(email, captcha string) error {
	key := GenerateCaptcha(email)                   // 根据邮箱地址生成验证码captcha在 Redis 的存储键key（前缀+邮箱）
	expire := 5 * time.Minute                       // 5分钟过期时间
	err := Rdb.Set(ctx, key, captcha, expire).Err() // 存储到redis
	if err != nil {
		log.Println("[SetCaptchaForEmail] Rdb Set error: ", err)
	} else {
		log.Println("[SetCaptchaForEmail] Rdb Set success: ", key)
	}
	return err
}

// CheckCaptchaForEmail 验证邮箱验证码是否正确
//
// 参数：
//
//	email: 用户邮箱地址
//	userInput: 用户输入的验证码
//
// 返回值：
//
//	bool: 验证码是否正确
//	error: 操作过程中发生的错误（如Redis连接问题）
//
// 注意：验证成功后会自动删除Redis中的验证码，验证码比较不区分大小写
func CheckCaptchaForEmail(email, userInput string) (bool, error) {
	key := GenerateCaptcha(email) // 生成验证码的key

	// 1. 获取存储的验证码
	storedCaptcha, err := Rdb.Get(ctx, key).Result() // 查询redis
	if err != nil {
		if err == redis.Nil {
			// 验证码不存在（可能过期）
			log.Println("[CheckCaptchaForEmail] Rdb Get error: ", err)
			return false, err
		}
		// 其他错误
		return false, err
	}

	// 2. 比较验证码（不区分大小写）
	if strings.EqualFold(storedCaptcha, userInput) {
		// 验证成功，删除 key
		log.Println("[CheckCaptchaForEmail] EqualFold success")
		if err := Rdb.Del(ctx, key).Err(); err != nil {
			// 删除失败
			log.Println("[CheckCaptchaForEmail] Rdb Del error: ", err)
		} else {
			// 删除成功
			log.Println("[CheckCaptchaForEmail] Rdb Del success: ", key)
		}
		return true, nil
	} else {
		// 验证失败
		log.Println("[CheckCaptchaForEmail] EqualFold error")
		return false, nil
	}
}

func AcquireLock(ctx context.Context, key string, token string, expiration time.Duration) (bool, error) {
	return Rdb.SetNX(ctx, key, token, expiration).Result()
}

func ReleaseLock(ctx context.Context, key string, token string) error {
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`
	return Rdb.Eval(ctx, script, []string{key}, token).Err()
}

// InitRedisIndex 初始化 Redis 索引，支持按文件名区分
func InitRedisIndex(ctx context.Context, filename string, dimension int) error {
	// 生成索引名称
	indexName := GenerateIndexName(filename)

	// 检查索引是否存在
	_, err := Rdb.Do(ctx, "FT.INFO", indexName).Result()
	if err == nil {
		log.Println("[InitRedisIndex] 索引已存在，跳过创建")
		return nil
	}

	if isRediSearchUnsupported(err) {
		return fmt.Errorf("当前 Redis 不支持 RediSearch/向量索引，请使用 Redis Stack 或启用 RediSearch 模块")
	}

	// 如果索引不存在，创建新索引
	if !strings.Contains(err.Error(), "Unknown index name") {
		return fmt.Errorf("检查索引失败: %w", err)
	}

	log.Println("[InitRedisIndex] 正在创建 Redis 索引...")

	// 生成索引名称前缀
	prefix := GenerateIndexNamePrefix(filename)

	// 创建索引
	createArgs := []interface{}{
		"FT.CREATE", indexName, // 创建一个新的索引，名字是 indexName
		"ON", "HASH",
		"PREFIX", "1", prefix,
		"SCHEMA",
		"content", "TEXT", // 定义字段类型。content 和 metadata 都是文本字段
		"metadata", "TEXT",
		"vector", "VECTOR", "FLAT", // 告诉 Redis 这是一个向量字段，存储向量数据。
		"6",
		"TYPE", "FLOAT32", // 向量里每个数字的类型
		"DIM", dimension, // 向量的维度
		"DISTANCE_METRIC", "COSINE", // 告诉 Redis，查相似向量的时候，用“余弦相似度”去比较两个向量是不是意思相近
	}

	// 执行创建索引命令
	if err := Rdb.Do(ctx, createArgs...).Err(); err != nil {
		if isRediSearchUnsupported(err) {
			return fmt.Errorf("当前 Redis 不支持 RediSearch/向量索引，请使用 Redis Stack 或启用 RediSearch 模块")
		}
		return fmt.Errorf("创建索引失败: %w", err)
	}

	log.Println("[InitRedisIndex] 索引创建成功！")
	return nil
}

// DeleteRedisIndex 删除 Redis 索引和键值对，支持按文件名区分
func DeleteRedisIndex(ctx context.Context, filename string) error {
	indexName := GenerateIndexName(filename)
	prefix := GenerateIndexNamePrefix(filename)
	if err := Rdb.Do(ctx, "FT.DROPINDEX", ChineseKeywordIndexName(filename)).Err(); err != nil && !strings.Contains(err.Error(), "Unknown Index name") && !strings.Contains(err.Error(), "Unknown index name") {
		return fmt.Errorf("删除关键词索引失败: %w", err)
	}

	// 1. 先删除索引
	if err := Rdb.Do(ctx, "FT.DROPINDEX", indexName).Err(); err != nil {
		if isRediSearchUnsupported(err) {
			return fmt.Errorf("当前 Redis 不支持 RediSearch/向量索引，请使用 Redis Stack 或启用 RediSearch 模块")
		}
		// 索引不存在可能不是错误，继续执行
		log.Printf("[DeleteRedisIndex] 删除索引失败或索引不存在: %v", err)
	} else {
		log.Printf("[DeleteRedisIndex] 索引 %s 删除成功！\n", indexName)
	}

	// 2. 删除对应的数据键值对
	if err := DeleteRedisDataByPrefix(ctx, prefix); err != nil {
		return fmt.Errorf("[DeleteRedisIndex] 删除数据失败: %w", err)
	}

	return nil
}

// DeleteRedisDataByPrefix 删除指定前缀的所有 Redis 数据键
func DeleteRedisDataByPrefix(ctx context.Context, prefix string) error {
	var cursor uint64
	pattern := prefix + "*"

	for {
		// 使用 SCAN 命令遍历匹配前缀的键
		keys, nextCursor, err := Rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("扫描 Redis 键失败: %w", err)
		}

		// 删除找到的键
		if len(keys) > 0 {
			if err := Rdb.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("删除 Redis 键失败: %w", err)
			}
			log.Printf("[DeleteRedisDataByPrefix] 删除了 %d 个键\n", len(keys))
		}

		// 如果游标回到 0，表示遍历完成
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	log.Printf("[DeleteRedisDataByPrefix] 成功删除前缀 %s 的所有数据\n", prefix)
	return nil
}

func CountRedisDataByPrefix(ctx context.Context, prefix string, sampleLimit int) (int, []string, error) {
	var cursor uint64
	pattern := prefix + "*"
	count := 0
	samples := make([]string, 0, sampleLimit)

	for {
		keys, nextCursor, err := Rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return 0, nil, fmt.Errorf("扫描 Redis 键失败: %w", err)
		}

		count += len(keys)
		if sampleLimit > 0 && len(samples) < sampleLimit {
			remaining := sampleLimit - len(samples)
			if len(keys) < remaining {
				remaining = len(keys)
			}
			samples = append(samples, keys[:remaining]...)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return count, samples, nil
}
