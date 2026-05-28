package rabbitmq

// 全局变量
var (
	RMQMessage *RabbitMQ
)

// 创建 MQ 并启动消费者
// 无论调用多少次 NewWorkRabbitMQ，只会创建一次连接 Connection，共享同一个底层连接
// 不同队列共用一个连接 Connection，可以保持不同队列消费消息的顺序
func InitRabbitMQ() {
	RMQMessage = NewWorkRabbitMQ("Message") // queue 名称 Message
	go RMQMessage.Consume(MQMessage)        // 使用 go 关键字启动 goroutine 来异步消费消息
}

// DestroyRabbitMQ 销毁RabbitMQ
func DestroyRabbitMQ() {
	RMQMessage.Destroy()
}
