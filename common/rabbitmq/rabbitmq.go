package rabbitmq

import (
	"GoAI/config"
	"fmt"
	"log"

	"github.com/streadway/amqp"
)

// 单例模式
// 全局connection对象
// 所有RabbitMQ都会复用该对象
var conn *amqp.Connection

// 初始化connection
func initConn() {
	// 从配置文件读取RabbitMQ连接参数，构建连接URL
	c := config.GetConfig()
	mqUrl := fmt.Sprintf(
		"amqp://%s:%s@%s:%d/%s",
		c.RabbitmqUsername, c.RabbitmqPassword, c.RabbitmqHost, c.RabbitmqPort, c.RabbitmqVhost,
	)
	log.Printf("Connecting to RabbitMQ at %s:%d", c.RabbitmqHost, c.RabbitmqPort)
	var err error
	conn, err = amqp.Dial(mqUrl) // 创建连接
	if err != nil {
		log.Fatalf("RabbitMQ connection failed: %v", err) // 输出错误并退出程序
	}
}

// RabbitMQ 结构体
//
// RabbitMQ RabbitMQ结构体
type RabbitMQ struct {
	conn     *amqp.Connection // 复用 connection
	channel  *amqp.Channel    // AMQP 通道对象
	Exchange string           // 使用的交换机名称
	Key      string           // 路由键
}

// NewRabbitMQ 创建RabbitMQ对象
func NewRabbitMQ(exchange string, key string) *RabbitMQ {
	return &RabbitMQ{Exchange: exchange, Key: key}
}

// Destroy 断开 channel 和 connection
func (r *RabbitMQ) Destroy() {
	_ = r.channel.Close()
	_ = r.conn.Close()
}

// NewWorkRabbitMQ 创建 Work 模式的 RabbitMQ 实例RabbitMQ 实例, 连接(Connection)是单例的，而通道(Channel)是多实例的
func NewWorkRabbitMQ(queue string) *RabbitMQ {
	// new rabbitmq
	rabbitmq := NewRabbitMQ("", queue) // 使用默认交换机

	// get connection - 这里是单例
	// 只有当 conn 为 nil 时才会调用 initConn() 创建新连接，否则直接复用现有连接
	if conn == nil {
		initConn() // 整个应用程序中只有一个 connection 对象
	}
	rabbitmq.conn = conn

	// get channel - 每次调用都会创建新的 channel
	var err error
	rabbitmq.channel, err = rabbitmq.conn.Channel()
	if err != nil {
		panic(err.Error())
	}

	return rabbitmq
}

// Publish 发送消息 生产者
//
// 发布消息到队列，会先确保队列存在，然后使用默认交换机发送消息。这里使用了队列名作为路由键
func (r *RabbitMQ) Publish(message []byte) error {
	// 创建队列（不存在时）
	_, err := r.channel.QueueDeclare(
		r.Key, // 队列名称
		false, // 是否持久化
		false, // 是否自动删除
		false, // 是否排他队列
		false, // 是否等待服务器确认
		nil,   // 额外属性
	)
	if err != nil {
		return err
	}

	// 调用 channel 发送消息到队列
	return r.channel.Publish(
		r.Exchange, // 交换机
		r.Key,      // 路由键
		false,      // mandatory
		false,      // immediate
		amqp.Publishing{
			ContentType: "text/plain",
			Body:        message,
		},
	)
}

// Consume 消费者
//
//	handle: 消息的消费业务函数，用于消费消息
//	handle实际传进来的是 MQMessage 函数
//	实现了消费者逻辑，创建队列并开始消费消息。采用回调函数的方式处理接收到的消息
func (r *RabbitMQ) Consume(handle func(msg *amqp.Delivery) error) {
	// 创建队列
	q, err := r.channel.QueueDeclare(
		r.Key, // 队列名称
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		panic(err)
	}

	// 接收消息
	msgs, err := r.channel.Consume(
		q.Name, // 队列名称
		"",     // 消费者标签
		true,   // 自动确认
		false,  // 排他队列
		false,  // noLocal
		false,  // 是否等待服务器确认
		nil,    // 额外属性
	)
	if err != nil {
		panic(err)
	}

	// 处理消息
	for msg := range msgs {
		if err := handle(&msg); err != nil {
			log.Println("[RabbitMQ.Consume] " + err.Error())
		}
	}
}
