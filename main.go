package main

import (
	"GoAI/common/aihelper"
	"GoAI/common/mysql"
	"GoAI/common/rabbitmq"
	"GoAI/common/redis"
	"GoAI/config"
	"GoAI/dao/message"
	"GoAI/router"
	"fmt"
	"log"
)

// StartServer 启动HTTP服务器并监听指定的地址和端口
// 参数:
//
//	addr - 服务器监听地址
//	port - 服务器监听端口
//
// 返回值:
//
//	error - 如果服务器启动失败则返回错误
func StartServer(addr string, port int) error {
	r := router.InitRouter()
	//服务器静态资源路径映射关系，这里目前不需要
	// r.Static(config.GetConfig().HttpFilePath, config.GetConfig().MusicFilePath)
	return r.Run(fmt.Sprintf("%s:%d", addr, port))
}

// 从数据库加载消息并初始化 AIHelperManager
func readDataFromDB() error {
	// 获取全局管理器实例，单例模式
	manager := aihelper.GetGlobalManager()
	// 从数据库读取所有消息
	msgs, err := message.GetAllMessages()
	if err != nil {
		return err
	}
	// 遍历数据库消息
	for i := range msgs {
		m := &msgs[i]
		//默认qwen模型
		modelType := "qwen"
		config := make(map[string]interface{})
		// 创建或获取对应的 AIHelper 实例
		helper, err := manager.GetOrCreateAIHelper(m.UserName, m.SessionID, modelType, config)
		if err != nil {
			log.Printf("[readDataFromDB] failed to create helper for user=%s session=%s: %v", m.UserName, m.SessionID, err)
			continue
		}
		log.Println("[readDataFromDB] init: ", helper.SessionID)
		// 添加消息到内存中(不开启存储功能)
		helper.AddMessage(m.Content, m.UserName, m.IsUser, false)
	}

	log.Println("[AIHelperManager] init success ")
	return nil
}

func main() {
	// 初始化 config
	conf := config.GetConfig()
	host := conf.MainConfig.Host
	port := conf.MainConfig.Port

	// 初始化 mysql
	if err := mysql.InitMysql(); err != nil {
		log.Println("[InitMysql] error , " + err.Error())
		return
	} else {
		log.Println("[InitMysql] success ")
	}

	// 初始化 AIHelperManager，并从数据库加载历史消息数据
	if err := readDataFromDB(); err != nil {
		log.Println("[readDataFromDB] error , " + err.Error())
		return
	} else {
		log.Println("[readDataFromDB] success ")
	}

	// 初始化 redis
	if err := redis.Init(); err != nil {
		log.Println("[redis] init error , " + err.Error())
		return
	} else {
		log.Println("[redis] init success ")
	}

	// 初始化 RabbitMQ
	rabbitmq.InitRabbitMQ()
	log.Println("[RabbitMQ] init success ")

	// 启动 HTTP 服务
	err := StartServer(host, port)
	if err != nil {
		panic(err)
	}
}
