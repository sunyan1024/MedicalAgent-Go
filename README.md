# Go项目：AI应用服务平台（GoAI）

## 个人知识库、企业知识库与管理员

- **个人知识库**：原知识库入口，文件仅本人可管理和检索，管理员也不能访问其他用户的个人文件。
- **企业知识库**：平台共用一个企业资料库，所有登录用户可查看文件列表、在 RAG 问答中使用；只有管理员可以上传、秒传或删除文件。
- **RAG 问答**：自动联合检索本人个人文件和企业文件。同一物理文件只检索一次，优先使用个人文件的展示名称；文件变更在下一次问答生效。普通聊天模式不自动检索。
- 两个知识库共用物理文件与索引，同一库内禁止重复上传。删除一侧引用不会影响另一侧，最后一个引用删除后才清理索引。

### 初始化管理员

在 `config/config.toml` 中填写以下配置，再运行 `go run .` 启动服务。示例值需要替换，不提供内置管理员或默认密码。

```toml
[adminConfig]
username = "your-admin-name"
email = "admin@example.com"
password = "<your-password>"
```

管理员使用现有登录页，以用户名或邮箱登录；菜单会显示管理员标识。首次启动创建账号并使用 bcrypt 保存密码，重复启动不会修改现有管理员密码。三个配置项全部留空时跳过创建；部分配置、无效邮箱或账号冲突会终止启动，不会把已有普通账号自动提升为管理员。初始化成功后可清空这三个配置项，账号保留。

当前版本不包含多企业、成员管理、用户管理后台及在线角色变更功能。公开注册始终创建普通用户。

### 迁移与接口

启动时迁移历史用户为 `role=user`，历史知识库文件归入原用户的个人知识库，保留文件 ID、物理文件和已有索引。迁移先回填归属，再建立 `(scope, owner_key, stored_name)` 唯一索引，最后移除旧索引；可重复执行。升级时停止旧后端实例后启动新版本，避免旧版本继续写入缺少归属的文件记录。

| 接口 | 权限 |
| --- | --- |
| `GET /api/v2/user/me` | 登录用户；返回 `user_info`，包含 `role` |
| `/api/v2/file/list`、`precheck`、`upload`、`DELETE /:id` | 原个人文件接口，仅本人 |
| `GET /api/v2/enterprise/file/list` | 所有登录用户 |
| `POST /api/v2/enterprise/file/precheck`、`upload` | 管理员 |
| `DELETE /api/v2/enterprise/file/:id` | 管理员 |

企业文件接口沿用个人文件请求及响应格式。后端从数据库读取当前角色，管理员写操作权限不足返回 HTTP 403 和 `status_code=3001`；前端角色显示不能作为权限依据。文件归属由服务端生成。

### 验证

```sh
go test ./...
# 可选集成测试：读取本地 config/config.toml 的连接配置。
# 自动创建、清理随机命名的临时 MySQL 库，不迁移应用库；需要 CREATE/DROP DATABASE 权限。
# Redis 测试启动独立临时进程（随机本机端口，关闭持久化），不访问应用 Redis。
GOPHERAI_TEST_MYSQL=1 GOPHERAI_TEST_REDIS=1 go test ./common/mysql ./service/user ./dao/file ./service/file ./router -count=1
cd vue-frontend
npm run lint -- --no-fix
npm run build
```

集成测试覆盖历史迁移及重复迁移、管理员初始化及冲突、个人文件隔离、企业写权限及角色实时生效、跨库秒传、重复删除和共享索引清理。Redis 集成测试默认使用 `/opt/redis-stack/bin/redis-server` 和 `/opt/redis-stack/lib/redisearch.so`，可通过 `GOPHERAI_TEST_REDIS_SERVER`、`GOPHERAI_TEST_REDISEARCH_MODULE` 指定安装路径。默认测试不调用真实模型。

页面验收：分别登录两个普通账号和管理员；检查个人文件互不可见，普通账号的企业页面没有上传和删除按钮，管理员可维护企业资料。分别上传仅个人资料和仅企业资料，在 RAG 模式中提问验证两类资料均可检索，再验证流式和非流式响应；管理员删除企业文件后，新问答不再检索该文件。历史回答保留原内容。

## 🚀 项目亮点：构建一个真正的 AI 应用服务平台

**AI 多轮对话（上下文记忆 + 会话持久化**）

基于 Gin + MySQL + RabbitMQ 实现多会话体系，AI 能记住你的历史上下文，实现完整的“AI 聊天助手”能力。对话流式输出（SSE）丝滑无阻塞。

**图像识别（ONNXRuntime + MobileNetV2 全流程**）

图片上传 → 数据预处理 → ONNX 推理 → 分类标签输出，完整还原真实业务中的 AI 图像推理链路，不仅能跑，还能扩展成视觉服务。

**工程级后端架构（真正能上线**）

整合 Gin / MySQL / Redis / RabbitMQ / GORM，涵盖 Web 开发的所有关键组件，认证、缓存、异步任务、错误处理全都有。

**高并发与异步化设计（性能导向架构**）

聊天消息写入通过 RabbitMQ 异步化处理，流式响应提升用户体验，后台任务不阻塞主线程，AI 调用路径更高效稳定，符合高并发系统设计标准。

**AI 工厂模式（更专业的架构设计**）

通过工厂模式实现多模型接入的可扩展架构：新增模型只需新增一个 Provider，完全开放性、稳定性与可维护性兼顾。

**全栈体验（Go 后端 + Vue3 前端**）

从 API → 前端 UI 全链路开发：登录注册、验证码、会话管理、图片上传、AI 对话，都有对应前端界面。
