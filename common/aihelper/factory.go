package aihelper

import (
	"context"
	"fmt"
	"sync"
)

// ModelCreator 定义模型创建函数类型（需要 context）
type ModelCreator func(ctx context.Context, config map[string]interface{}) (AIModel, error)

// 工厂模式
// AIModelFactory AI模型工厂
type AIModelFactory struct {
	creators map[string]ModelCreator // map存储不同类型模型对应的创建函数
	mu       sync.RWMutex
}

// 单例模式
// 全局工厂实例
var (
	globalFactory *AIModelFactory
	factoryOnce   sync.Once // sync.Once 确保工厂实例只初始化一次
)

// GetGlobalFactory 获取全局单例
func GetGlobalFactory() *AIModelFactory {
	factoryOnce.Do(func() {
		globalFactory = &AIModelFactory{
			creators: make(map[string]ModelCreator),
		}
		globalFactory.registerCreators() // 初始化时自动注册支持的模型类型
	})
	return globalFactory
}

// 注册模型
// 将不同模型的创建函数注册到工厂的creators映射中，使用字符串标识符区分不同模型。
func (f *AIModelFactory) registerCreators() {
	f.mu.Lock()
	defer f.mu.Unlock()

	//OpenAI 类型接口（阿里百炼）
	f.creators["qwen"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		return NewQwenModel(ctx)
	}

	//Ollama 本地部署
	f.creators["ollama"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		return NewOllamaModel(ctx)
	}

	// 阿里百炼 RAG 模型
	f.creators["rag"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		username, ok := config["username"].(string)
		if !ok {
			return nil, fmt.Errorf("RAG model requires username")
		}
		return NewAliRAGModel(ctx, username)
	}

	// 阿里百炼 MCP 模型（集成MCP服务）
	f.creators["mcp"] = func(ctx context.Context, config map[string]interface{}) (AIModel, error) {
		return NewMCPModel(ctx)
	}
}

// CreateAIModel 根据类型创建 AI 模型
// 根据传入的模型类型查找对应的创建函数并执行，传递配置参数。
func (f *AIModelFactory) CreateAIModel(ctx context.Context, modelType string, config map[string]interface{}) (AIModel, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	creator, ok := f.creators[modelType]
	if !ok {
		return nil, fmt.Errorf("unsupported model type: %s", modelType)
	}
	return creator(ctx, config)
}

// CreateAIHelper 一键创建 AIHelper
// 更高层次的封装，不仅创建AI模型，还直接创建AIHelper实例，简化使用流程。
func (f *AIModelFactory) CreateAIHelper(ctx context.Context, modelType string, SessionID string, config map[string]interface{}) (*AIHelper, error) {
	model, err := f.CreateAIModel(ctx, modelType, config)
	if err != nil {
		return nil, err
	}
	return NewAIHelper(model, SessionID), nil
}

// RegisterModel 可扩展注册
// 允许外部动态注册新的模型类型，提供了良好的扩展性，使系统能够轻松添加新的AI模型支持。
func (f *AIModelFactory) RegisterModel(modelType string, creator ModelCreator) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.creators[modelType] = creator
}
