package config

import (
	"log"

	"github.com/BurntSushi/toml"
)

type MainConfig struct {
	Port    int    `toml:"port"`
	AppName string `toml:"appName"`
	Host    string `toml:"host"`
}

type EmailConfig struct {
	Authcode string `toml:"authcode"`
	Email    string `toml:"email" `
}

type RedisConfig struct {
	RedisPort     int    `toml:"port"`
	RedisDb       int    `toml:"db"`
	RedisHost     string `toml:"host"`
	RedisPassword string `toml:"password"`
}

type MysqlConfig struct {
	MysqlPort         int    `toml:"port"`
	MysqlHost         string `toml:"host"`
	MysqlUser         string `toml:"user"`
	MysqlPassword     string `toml:"password"`
	MysqlDatabaseName string `toml:"databaseName"`
	MysqlCharset      string `toml:"charset"`
}

type JwtConfig struct {
	ExpireDuration int    `toml:"expire_duration"`
	Issuer         string `toml:"issuer"`
	Subject        string `toml:"subject"`
	SecretKey      string `toml:"key"`
}

type Rabbitmq struct {
	RabbitmqPort     int    `toml:"port"`
	RabbitmqHost     string `toml:"host"`
	RabbitmqUsername string `toml:"username"`
	RabbitmqPassword string `toml:"password"`
	RabbitmqVhost    string `toml:"vhost"`
}

type QwenConfig struct {
	QwenApiKey     string `toml:"api_key"`
	QwenModelName  string `toml:"modelname"`
	QwenBaseUrl    string `toml:"baseurl"`
	EnableThinking bool   `toml:"enable_thinking"`
}

type RagModelConfig struct {
	RagApiKey            string `toml:"api_key"`
	RagBaseUrl           string `toml:"baseUrl"`
	RagChatModelName     string `toml:"chatModelName"`
	RagEmbeddingModel    string `toml:"embeddingModel"`
	RagDimension         int    `toml:"dimension"`
	RagDocDir            string `toml:"docDir"`
	EnableThinking       bool   `toml:"enable_thinking"`
	RerankEnabled        bool   `toml:"rerankEnabled"`
	RerankModel          string `toml:"rerankModel"`
	RerankBaseUrl        string `toml:"rerankBaseUrl"`
	RerankCandidateTopK  int    `toml:"rerankCandidateTopK"`
	RerankTopK           int    `toml:"rerankTopK"`
	RerankTimeoutSeconds int    `toml:"rerankTimeoutSeconds"`
	OCRAccessToken       string `toml:"ocrAccessToken"`
	OCRTimeoutSeconds    int    `toml:"ocrTimeoutSeconds"`
	OCRModel             string `toml:"ocrModel"`
}

type OllamaConfig struct {
	OllamaModelName string `toml:"modelname"`
	OllamaBaseUrl   string `toml:"baseurl"`
	EnableThinking  bool   `toml:"enable_thinking"`
}

type TTSConfig struct {
	TTSBaseUrl      string `toml:"baseurl"`
	TTSTokenUrl     string `toml:"tokenurl"`
	TTSClientId     string `toml:"client_id"`
	TTSClientSecret string `toml:"client_secret"`
}

type MCPConfig struct {
	MCPBaseUrl string `toml:"baseUrl"`
}

type Config struct {
	EmailConfig    `toml:"emailConfig"`
	RedisConfig    `toml:"redisConfig"`
	MysqlConfig    `toml:"mysqlConfig"`
	JwtConfig      `toml:"jwtConfig"`
	MainConfig     `toml:"mainConfig"`
	Rabbitmq       `toml:"rabbitmqConfig"`
	QwenConfig     `toml:"qwenConfig"`
	RagModelConfig `toml:"ragModelConfig"`
	OllamaConfig   `toml:"ollamaConfig"`
	TTSConfig      `toml:"ttsConfig"`
	MCPConfig      `toml:"mcpConfig"`
}

// 管理 Redis 键名前缀
type RedisKeyConfig struct {
	CaptchaPrefix   string // 存储验证码相关数据在 Redis 中的键名前缀模式
	IndexName       string
	IndexNamePrefix string
}

// RedisKeyConfig 全局变量
var DefaultRedisKeyConfig = RedisKeyConfig{
	CaptchaPrefix:   "captcha:%s",
	IndexName:       "rag_docs:%s:idx",
	IndexNamePrefix: "rag_docs:%s:",
}

// 全局变量 单例模式确保配置唯一性
var config *Config

// InitConfig 初始化项目配置
func InitConfig() error {
	// 设置配置文件路径（相对于 main.go 所在的目录）
	// 解析 toml
	if _, err := toml.DecodeFile("config/config.toml", config); err != nil {
		log.Fatal(err.Error())
		return err
	}
	return nil
}

// 单例模式
func GetConfig() *Config {
	if config == nil {
		config = new(Config)
		_ = InitConfig()
	}
	return config
}
