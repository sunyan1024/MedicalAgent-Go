# 真实 HTTP 新旧版本对照

在项目根目录运行：

```sh
python3 scripts/http_compare/run.py --output /tmp/medical-http-comparison-新的目录
```

需要本项目 `config/config.toml` 的 MySQL、RabbitMQ、Redis、Qwen 可用。数据库账号须能创建、删除以 `httpcmp_` 开头的临时数据库。实验调用真实付费模型；每版最多 40 次模型 HTTP 请求，连续三次供应商请求错误停止，总时限 30 分钟。模型参数由两个提交的生产工厂原样生成。

固定提交：新版 `63698453bde8d658fa0897d6a345db5e866176d5`，旧版 `13368e2bc06d8345462cc63338096149473e2a5a`。在 `/tmp` 独立 worktree 构建同一个 `launcher.go`，使用 `router.InitRouter()` 启动独立 TCP 服务。仅启动、资源隔离、发送边界采集和进程控制属于夹具；未替换生产模型、检索、Helper、Agent Runtime 或 MQ 消费回调。

合成账号通过夹具直接播种 bcrypt 密码，随后使用生产 `/user/login` 获取 JWT。账号注册、验证码和邮件链路不在实验范围内。管理动作通过子进程标准输入执行，不增加测试 HTTP 路由。随机密码及 JWT 仅存在内存/进程管道，日志输出丢弃；不保存认证头和 API key。默认配置文件只从原工作区读取，worktree 中写入空配置占位，随后在内存加载原配置并覆盖临时数据库名。

每版使用独立数据库和随机队列，原样调用 `NewWorkRabbitMQ` 和 `MQMessage`。每版三组六轮聊天，第六轮通过应用非流式接口；该接口的 Agent 内部仍可使用模型流式请求，按生产实现记录。第二组反转版本请求顺序。每组完成后轮询数据库消息数量，再对照 Helper、历史接口、数据库的完整内容和用户/助手顺序，检查非聊天过程未额外落库。

输出目录包含：

- `new/`、`old/`：配置摘要、逐轮问题与真实回答、逐帧 SSE 时间和原始 `.sse`、模型 HTTP 请求/响应、原始 usage 与请求 ID、记忆更新、Helper/数据库快照、历史接口和隔离检查。
- `metrics.json`：缓存 token 加权比例、调用次数、输出字符数、completion tokens、正文延迟和总耗时。
- `paired.json`：每组每轮新版减旧版的配对差值。
- `validations.json`：历史、落库和账号隔离验收。
- `status.json`、`报告.md`：完成/中止状态、清理结果与中文报告。

`run_success` 按真实枚举 `completed` 验证；`partial` 即使返回 HTTP 200 和 `[DONE]` 也不算成功。记忆更新后，历史中旧回答仍可包含 `PREF_A`，不能把历史旧码误判为最新记忆未更新。

重新汇总已有证据，不发请求：

```sh
python3 scripts/http_compare/run.py --summarize /tmp/medical-http-comparison-已有目录
GOCACHE=/tmp/go-build-cache go test ./scripts/http_compare -count=1
python3 -m unittest discover -s scripts/http_compare -p 'test_*.py'
```

正常或异常退出时先关闭 HTTP 服务及队列连接，再由独立清理进程删除本次队列与数据库，最后移除 worktree。强制杀死父进程、断电等无法运行 finally 的情况可能遗留资源；具体随机名称见各版 `config-summary.json`。Redis 使用原配置，不主动写入/清除共享键，不准备医学知识库或报告。

此实验验证应用 HTTP 链路，不验证浏览器、反向代理或重启后恢复。真实回答会造成后续输入分叉，小样本和供应商缓存波动限制了性能结论。
