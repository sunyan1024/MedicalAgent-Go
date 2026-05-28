package main

import (
	"context"
	"flag"
	"log"
	"os"
	"time"

	mcpclient "GoAI-mcp/client"
	mcpserver "GoAI-mcp/server"
)

func main() {
	// 定义命令行标志
	mode := flag.String("mode", "", "运行模式: server 或 client")
	httpAddr := flag.String("http-addr", ":8081", "HTTP服务器地址")
	city := flag.String("city", "", "要查询天气的城市名称")
	flag.Parse()

	if *mode == "" {
		log.Println("Error: 您必须指定模式使用-mode (server 或 client)")
		flag.Usage()
		os.Exit(1)
	}

	if *mode == "server" {
		// 启动服务器
		log.Println("启动MCP服务器...")
		if err := mcpserver.StartServer(*httpAddr); err != nil {
			log.Fatalf("服务器错误: %v", err)
		}

	} else if *mode == "client" {
		// 运行客户端
		if *city == "" {
			log.Println("Error: 您必须指定城市名称使用-city")
			flag.Usage()
			os.Exit(1)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// 创建客户端
		httpURL := "http://localhost:8081/mcp"
		mcpClient, err := mcpclient.NewMCPClient(httpURL)
		if err != nil {
			log.Fatalf("创建客户端失败: %v", err)
		}
		defer mcpClient.Close()

		// 初始化客户端
		if _, err := mcpClient.Initialize(ctx); err != nil {
			log.Fatalf("初始化失败: %v", err)
		}

		// 执行健康检查
		if err := mcpClient.Ping(ctx); err != nil {
			log.Fatalf("Ping失败: %v", err)
		}

		// 调用天气工具
		result, err := mcpClient.CallWeatherTool(ctx, *city)
		if err != nil {
			log.Fatalf("调用工具失败: %v", err)
		}

		// 显示天气结果
		log.Println("\n天气查询结果:")
		log.Println(mcpClient.GetToolResultText(result))

		log.Println("\n客户端初始化成功。正在关闭...")
	}
}
