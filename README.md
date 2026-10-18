# Go项目：AI应用服务平台（GoAI）

当前项目版本：`v0.3`。

## 本地配置

首次运行前复制配置模板：

```sh
cp config/config.example.toml config/config.toml
```

在 `config/config.toml` 中填写本地服务连接信息、模型 API Key 和随机生成的 JWT 签名密钥，再启动项目。管理员配置三项可全部留空。此文件已加入 `.gitignore`，请勿提交真实凭据。

前端依赖通过 `cd vue-frontend && npm ci` 安装，`node_modules` 不随源码发布。RAG 评测脚本需要通过环境变量 `GOAI_TOKEN` 或 `--token` 提供登录 JWT，不包含默认令牌。

## 日志文件

主 Go 后端日志同时输出到终端和工作目录下的 `logs/YYYY-MM-DD.log`，目录自动创建。标准业务日志、GORM 日志和 Gin 请求、调试及 Recovery 错误日志共用每日文件；终端保留颜色，文件为带时间戳的纯文本。

按服务器本地日期切分，跨天后的第一条日志切换文件，同一天重启继续追加。历史文件全部保留，不自动清理；现有 `.gitignore` 的 `logs/*` 规则屏蔽日志文件。日志文件初始化失败会停止启动；运行中写入失败会向终端报错，后续日志继续尝试写入。前端开发服务器、独立 MCP 进程和进程原始 stdout/stderr 不在捕获范围内。

## 医疗 Agent Demo

新增医疗报告解读、症状咨询和医学知识检索，包含主 Agent、两个独立子 Agent、受限工具调用循环、MCP 检索、报告确认与运行轨迹。支持 [个人档案](docs/medical/MEMORY.md)：MySQL 保存每用户一篇 Markdown，Agent 自主读写，页面支持预览、编辑和清空。

启动、资料导入、接口、评测与录屏脚本见 [医疗 Demo 文档](docs/medical/README.md)。公共知识库在界面显示为“医学知识库”，仍沿用原管理员权限；原 RAG 行为保留。

## 个人知识库、企业知识库与管理员

- **个人知识库**：原知识库入口，普通用户的文件仅本人可管理和检索，管理员不能访问个人知识库。
- **企业知识库**：平台共用一个企业资料库，仅管理员可查看文件列表、上传、秒传、删除文件及管理文档切片；普通用户无企业知识库入口，但 RAG 问答仍可使用企业资料及展示引用。
- **RAG 问答**：自动联合检索本人个人文件和企业文件。同一物理文件只检索一次，优先使用个人文件的展示名称；文件变更在下一次问答生效。普通聊天模式不自动检索。
- 两个知识库共用物理文件与索引，同一库内禁止重复上传。删除一侧引用不会影响另一侧，最后一个引用删除后才清理索引。

### 初始化管理员

在 `config/config.toml` 中填写以下配置，再运行 `go run .` 启动服务。示例值需要替换，不提供内置管理员或默认密码。

```toml
[adminConfig]
uid = "your-admin-uid"
email = "admin@example.com"
password = "<your-password>"
```

管理员使用现有登录页，以用户名或邮箱登录；登录后直接进入企业知识库管理页，仅提供企业文件管理和退出登录，不提供聊天或个人知识库功能。首次启动创建账号并使用 bcrypt 保存密码，重复启动不会修改现有管理员密码。三个配置项全部留空时跳过创建；部分配置、无效邮箱或账号冲突会终止启动，不会把已有普通账号自动提升为管理员。初始化成功后可清空这三个配置项，账号保留。

当前版本不包含多企业、成员管理、用户管理后台及在线角色变更功能。公开注册始终创建普通用户。

### 迁移与接口

启动时迁移历史用户为 `role=user`，历史知识库文件归入原用户的个人知识库，保留文件 ID、物理文件和已有索引。迁移先回填归属，再建立 `(scope, owner_key, stored_name)` 唯一索引，最后移除旧索引；可重复执行。升级时停止旧后端实例后启动新版本，避免旧版本继续写入缺少归属的文件记录。

| 接口 | 权限 |
| --- | --- |
| `GET /api/v3/user/me` | 登录用户；返回 `user_info`，包含 `role` |
| `/api/v3/file/list`、`precheck`、`upload`、`DELETE /:id` | 普通用户，仅本人 |
| `/api/v3/AI/*` | 普通用户 |
| `GET /api/v3/enterprise/file/list` | 管理员 |
| `POST /api/v3/enterprise/file/precheck`、`upload` | 管理员 |
| `DELETE /api/v3/enterprise/file/:id` | 管理员 |

企业文件接口沿用个人文件请求及响应格式。后端从数据库读取当前角色，角色不符合接口要求时返回 HTTP 403 和 `status_code=3001`；前端角色显示不能作为权限依据。文件归属由服务端生成。

### 管理 RAG 切片

管理员在医学知识库文档行点击“切片管理”，通过抽屉搜索、分页查看正文，手动新增、编辑、禁用/恢复或永久删除切片。修改会同步影响所有引用同一物理文件的企业库与个人库，不修改原始上传文件或历史回答。新增默认启用，编辑禁用切片后仍保持禁用；保存正文时自动重新生成向量，生成失败保留旧正文与向量。切片可全部禁用或删除。

接口前缀为 `/api/v3/enterprise/file/:id/chunks`，`:id` 必须是企业库的知识文件 ID，`:chunk_id` 为 `doc_1` 形式的切片 ID。管理员不能使用个人库文件 ID 调用这些接口。

| 方法与相对路径 | 参数与返回 |
| --- | --- |
| `GET /` | `page` 默认 1、`page_size` 默认 20（最多 100）、`keyword` 正文子串（最多 200 字）、`enabled=true/false`；返回 `chunks/total/page/page_size` |
| `GET /:chunk_id` | 返回 `chunk` 完整正文、字数、状态、版本、来源标识与维护方式 |
| `POST /` | JSON `{"content":"新正文"}`；返回新增 `chunk` |
| `PUT /:chunk_id` | JSON `{"content":"新正文","version":1}`；返回更新后的 `chunk` |
| `PATCH /:chunk_id` | JSON `{"enabled":false,"version":1}`；返回更新后的 `chunk` |
| `DELETE /:chunk_id` | JSON `{"version":1}`；永久删除，无法恢复 |

所有响应包含 `status_code/status_msg`；正文最多 10,000 个 Unicode 字符且不能全为空白，手动正文作为单个切片保存。状态及正文修改递增 `version`；版本冲突或文件正在处理中返回 HTTP 409，参数错误返回 400，不存在或不属于企业库返回 404。保存冲突时保留输入，使用“重新加载”查看最新正文后再编辑。向量、Redis 键和服务器路径不通过切片接口返回。

已有切片默认启用、版本为 1，无需迁移 MySQL 或重新上传文件。Redis 的原有索引前缀只保留启用切片；禁用正文及向量移至 `rag_chunk_disabled:<indexKey>:`，递增编号保存在 `rag_chunk_meta:<indexKey>:`。这些数据与现有切片一样依赖 Redis 的持久化与备份配置。删除切片不会重用编号；最后一个文件引用删除时清理上述数据。此版本不提供整篇重新切分、批量操作或切片版本历史。

### 验证

```sh
go test ./...
# 可选集成测试：读取本地 config/config.toml 的连接配置。
# 自动创建、清理随机命名的临时 MySQL 库，不迁移应用库；需要 CREATE/DROP DATABASE 权限。
# Redis 测试启动独立临时进程（随机本机端口，关闭持久化），不访问应用 Redis。
GOPHERAI_TEST_MYSQL=1 GOPHERAI_TEST_REDIS=1 go test ./common/mysql ./service/user ./dao/file ./service/file ./router -count=1
cd vue-frontend
npm run test:chunks
npm run lint -- --no-fix
npm run build
```

集成测试覆盖历史迁移及重复迁移、管理员初始化及冲突、个人文件隔离、企业读写权限、管理员业务访问限制及角色实时生效、跨库秒传、重复删除和共享索引清理。切片测试覆盖双路检索启停、正文与向量原子更新、版本/锁冲突、共享引用及删除清理；`npm run test:chunks` 验证抽屉异步状态与失败保留输入，不替代浏览器布局验收。项目直接使用 Redis 8.10.2 Search，不兼容旧 Redis Stack 的错误格式。Redis 集成测试默认使用 PATH 中的 `redis-server` 和新版模块 `/usr/lib/redis/modules/redisearch.so`，可通过 `GOPHERAI_TEST_REDIS_SERVER`、`GOPHERAI_TEST_REDISEARCH_MODULE` 指定安装路径。默认测试不调用真实模型。

页面验收：分别登录两个普通账号和管理员；检查个人文件互不可见，普通账号没有企业知识库入口且不能直接打开企业页面，管理员直达企业管理页且无法访问普通用户业务页面。分别上传仅个人资料和仅企业资料，在 RAG 模式中提问验证两类资料均可检索，再验证流式和非流式响应；管理员删除企业文件后，新问答不再检索该文件。历史回答保留原内容。

## 🚀 项目亮点：构建一个真正的 AI 应用服务平台

**AI 多轮对话（上下文记忆 + 会话持久化**）

基于 Gin + MySQL + RabbitMQ 实现多会话体系，AI 能记住你的历史上下文，实现完整的“AI 聊天助手”能力。对话流式输出（SSE）丝滑无阻塞。

**工程级后端架构（真正能上线**）

整合 Gin / MySQL / Redis / RabbitMQ / GORM，涵盖 Web 开发的所有关键组件，认证、缓存、异步任务、错误处理全都有。

**高并发与异步化设计（性能导向架构**）

聊天消息写入通过 RabbitMQ 异步化处理，流式响应提升用户体验，后台任务不阻塞主线程，AI 调用路径更高效稳定，符合高并发系统设计标准。

**AI 工厂模式（更专业的架构设计**）

通过工厂模式实现多模型接入的可扩展架构：新增模型只需新增一个 Provider，完全开放性、稳定性与可维护性兼顾。

**全栈体验（Go 后端 + Vue3 前端**）

从 API → 前端 UI 全链路开发：登录注册、验证码、会话管理、知识库文件上传、AI 对话，都有对应前端界面。

用户字段：`uid` 为字符串主键（登录及数据归属标识），`nickname` 为可重复的自定义昵称；不再使用用户表的 `id`、`name`、`username`。登录支持 UID 或邮箱，注册可填写昵称。此版本面向已清空数据的数据库，旧 JWT 需重新登录获取。
