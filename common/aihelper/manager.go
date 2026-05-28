package aihelper

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// 全局上下文，给eino使用
var ctx = context.Background()

// AIHelperManager AI助手管理器，管理用户-会话-AIHelper的映射关系
type AIHelperManager struct {
	helpers map[string]map[string]*AIHelper // map[用户账号（唯一）]map[会话ID]*AIHelper
	mu      sync.RWMutex                    // 读写锁
}

// NewAIHelperManager 创建新的管理器实例
func NewAIHelperManager() *AIHelperManager {
	return &AIHelperManager{
		helpers: make(map[string]map[string]*AIHelper),
	}
}

// GetOrCreateAIHelper 获取或创建指定用户和会话的AIHelper
// 如果会话已存在且模型类型相同，直接返回现有AIHelper
// 如果会话已存在但模型类型不同，更新模型后返回
// 如果会话不存在，创建新的AIHelper
func (m *AIHelperManager) GetOrCreateAIHelper(userName string, sessionID string, modelType string, config map[string]interface{}) (*AIHelper, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查用户是否存在，不存在就创建
	userHelpers, exists := m.helpers[userName]
	if !exists {
		userHelpers = make(map[string]*AIHelper)
		m.helpers[userName] = userHelpers
	}

	// 检查会话是否已存在
	helper, exists := userHelpers[sessionID]
	if exists {
		if helper.model.GetModelType() == modelType {
			// 存在，且模型不变，直接返回
			return helper, nil
		} else {
			// 存在，但模型变更，创建新的AIModel并替换原有模型
			factory := GetGlobalFactory() // 工厂模式
			newModel, err := factory.CreateAIModel(ctx, modelType, config)
			if err != nil {
				return nil, err
			}
			helper.ChangeModelType(newModel)
			log.Printf("ChangeModelType: username %s sessionID %s old model %s new model %s", userName, sessionID, helper.model.GetModelType(), modelType)
			return helper, nil
		}
	}

	// 不存在，创建新的AIHelper
	factory := GetGlobalFactory() // 工厂模式
	helper, err := factory.CreateAIHelper(ctx, modelType, sessionID, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create AI helper for user %s, session %s, model %s: %w",
			userName, sessionID, modelType, err)
	}

	userHelpers[sessionID] = helper
	return helper, nil
}

// 获取指定用户的指定会话的AIHelper
func (m *AIHelperManager) GetAIHelper(userName string, sessionID string) (*AIHelper, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return nil, false
	}

	helper, exists := userHelpers[sessionID]
	return helper, exists
}

// 移除指定用户的指定会话的AIHelper
func (m *AIHelperManager) RemoveAIHelper(userName string, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return
	}

	delete(userHelpers, sessionID)

	// 如果用户没有会话了，清理用户映射
	if len(userHelpers) == 0 {
		delete(m.helpers, userName)
	}
}

// 获取指定用户的所有会话ID
func (m *AIHelperManager) GetUserSessions(userName string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	userHelpers, exists := m.helpers[userName]
	if !exists {
		return []string{}
	}

	sessionIDs := make([]string, 0, len(userHelpers))
	//取出所有的key
	for sessionID := range userHelpers {
		sessionIDs = append(sessionIDs, sessionID)
	}

	return sessionIDs
}

// 单例模式
// 全局管理器实例
var (
	globalManager *AIHelperManager
	managerOnce   sync.Once // 使用 sync.Once 确保全局只有一个管理器实例
)

// GetGlobalManager 获取全局管理器实例
func GetGlobalManager() *AIHelperManager {
	// once.Do 保证初始化操作只执行一次且线程安全
	managerOnce.Do(func() {
		globalManager = NewAIHelperManager() // 首次调用时才创建实例
	})
	return globalManager
}
