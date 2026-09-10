# Go项目推荐：AI应用服务平台（GoAI）

在上次在[知识星球](https://programmercarl.com/other/kstar.htm)里推出 C++ AI应用服务平台项目之后，很多录友反馈有没有Go版本的，因为市面上Go相关的AI项目也很少。

（Java版已经在路上，估计12月中旬会在星球里与大家见面）

所以这次就安排上了，准备了很久、在星球内部正式推出的 **Go 语言 AI 应用服务平台（GoAI）**。

* 把整个项目迁移到 **Go 语言**
* 用它强大的并发模型与丰富生态
* 用 **Gin + EINO（字节 AI 框架）** 构建真正可落地的 AI 系统
* 让你在短时间内完成一个可上线的 AI 平台

这不是简单做个 Demo，而是：

- 一个能跑、能扩展、能商用的 AI 服务平台
- 从 API 调用 → 模型流式输出 → 多会话 AI → 本地模型部署 → 图像识别 → 全栈上线 的全流程实战
- 带你实现属于自己的 ChatGPT / AI助手 / AI多功能平台

你将真正体验“工程化 AI 应用是怎么落地的”。

## 个人知识库、企业知识库与管理员

- **个人知识库**：原知识库入口，文件仅本人可管理和检索，管理员也不能访问其他用户的个人文件。
- **企业知识库**：平台共用一个企业资料库，所有登录用户可查看文件列表、在 RAG 问答中使用；只有管理员可以上传、秒传或删除文件。
- **RAG 问答**：自动联合检索本人个人文件和企业文件。同一物理文件只检索一次，优先使用个人文件的展示名称；文件变更在下一次问答生效。普通聊天模式不自动检索。
- 两个知识库共用物理文件与索引，同一库内禁止重复上传。删除一侧引用不会影响另一侧，最后一个引用删除后才清理索引。

### 初始化管理员

在启动后端的环境中设置以下变量，再按原方式启动服务。示例值需要替换，不提供内置管理员或默认密码。

```sh
export GOPHERAI_ADMIN_USERNAME='your-admin-name'
export GOPHERAI_ADMIN_EMAIL='admin@example.com'
export GOPHERAI_ADMIN_PASSWORD='<your-password>'
go run .
```

管理员使用现有登录页，以用户名或邮箱登录；菜单会显示管理员标识。首次启动创建账号并使用 bcrypt 保存密码，重复启动不会修改现有管理员密码。三个变量全部未配置时跳过创建；部分配置、无效邮箱或账号冲突会终止启动，不会把已有普通账号自动提升为管理员。初始化成功后可移除这些环境变量，账号保留。

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

## 🚀 项目亮点：你将构建一个真正的 AI 应用服务平台

**AI 多轮对话（上下文记忆 + 会话持久化**）

基于 Gin + MySQL + RabbitMQ 实现多会话体系，AI 能记住你的历史上下文，实现完整的“AI 聊天助手”能力。对话流式输出（SSE）丝滑无阻塞。

**图像识别（ONNXRuntime + MobileNetV2 全流程**）

图片上传 → 数据预处理 → ONNX 推理 → 分类标签输出，完整还原真实业务中的 AI 图像推理链路，不仅能跑，还能扩展成视觉服务。

**工程级后端架构（真正能上线**）

整合 Gin / MySQL / Redis / RabbitMQ / GORM，涵盖 Web 开发的所有关键组件，认证、缓存、异步任务、错误处理全都有。拿到项目即可直接部署生产。

**高并发与异步化设计（性能导向架构**）

聊天消息写入通过 RabbitMQ 异步化处理，流式响应提升用户体验，后台任务不阻塞主线程，AI 调用路径更高效稳定，符合高并发系统设计标准。

**AI 工厂模式（更专业的架构设计**）

通过工厂模式实现多模型接入的可扩展架构：新增模型只需新增一个 Provider，完全开放性、稳定性与可维护性兼顾。

**全栈体验（Go 后端 + Vue3 前端**）

从 API → 前端 UI 全链路开发：登录注册、验证码、会话管理、图片上传、AI 对话，都有对应前端界面。属于真正能跑起来的完整产品。

**简历效果（可直接写四条大亮点**）

* AI 对话引擎集成（多会话 + 流式输出）
* AI 图像模型推理（ONNXRuntime 部署 + 推理加速）
* 高并发后台（RabbitMQ 异步消息处理）
* 可生产部署的全栈 AI 服务平台

## 项目演示视频

![image](https://file1.kamacoder.com/i/web/2025-11-21_11-31-51.jpg)

![image](https://file1.kamacoder.com/i/web/2025-11-21_11-32-12.jpg)

![image](https://file1.kamacoder.com/i/web/2025-11-21_11-32-30.jpg)

![image](https://file1.kamacoder.com/i/web/2025-11-21_11-32-44.jpg)

## 项目适合谁？

* 想系统学习 Go 后端开发
* 想做自己的 AI 应用
* 想打造强力简历项目
* 想提升工程化能力
* 想进入 AI 工程 / 智能应用开发

##  学完能掌握什么？

- Go 工程体系
- AI 模型调用 & 流式输出
- 多会话聊天系统
- 图像识别能力
- Redis / RabbitMQ / MySQL 全家桶
- Docker 部署
- 模块化架构设计
- 高并发优化


## 📌 项目架构一览

![image](https://file1.kamacoder.com/i/web/2025-11-20_09-34-09.jpg)

这张架构图展示了 GoAI 整个系统的核心组成部分，包括：

业务服务、AI 推理、第三方平台、基础设施、消息队列、数据库，以及前后端交互流程。

你可以把它理解为：

“用户从输入一个问题 → 后端处理 → AI 推理 → 数据落库 → 前端实时显示” 的全链路流程图。


## 项目细节

本项目的代码和讲解专栏只分享在[知识星球](https://programmercarl.com/other/kstar.htm)里。

做本项目，所需要的基础：

![](https://file1.kamacoder.com/i/web/2025-11-20_09-37-49.jpg)

架构介绍：

![](https://file1.kamacoder.com/i/web/2025-11-20_09-38-50.jpg)

项目环境准备：
![](https://file1.kamacoder.com/i/web/2025-11-20_09-39-43.jpg)

各个模块细节：

![](https://file1.kamacoder.com/i/web/2025-11-20_09-41-28.jpg)

简历写法：

![](https://file1.kamacoder.com/i/web/2025-11-20_09-42-05.jpg)

本项目相关面试题以及如何应该如何回答，都给大家列好了：

![](https://file1.kamacoder.com/i/web/2025-11-20_09-43-19.jpg)

如果想突击做这个项目，直接把简历写法写到简历上，然后背面试题就好。

## 答疑

本项目在[知识星球](https://programmercarl.com/other/kstar.htm)里为 文字专栏形式，大家不用担心 看不懂，星球里每个项目有专属答疑群，任何问题都可以在群里问，都会得到解答：

![](https://file1.kamacoder.com/i/web/2025-09-26_11-30-13.jpg)

## 获取本项目专栏

本专栏仅为星球内部专享，大家可以加入[知识星球](https://programmercarl.com/other/kstar.htm)里获取。

项目内容在星球置顶一：

![](https://file1.kamacoder.com/i/web/20241218110921.png)

![](https://file1.kamacoder.com/i/web/2025-11-20_09-48-18.jpg)

### 加入[知识星球](https://programmercarl.com/other/kstar.htm)四大权益

1、**高质量项目合集（C++ / Java / Go / Python / AI**）

可以获得星球里 **20+ 套项目专栏资料，不仅有详细讲解，而且都配套专属答疑服务**。

星球里的 C++和Go版本的AI项目，在全网十分稀缺。

![](https://file1.kamacoder.com/i/web/2025-09-29_11-09-40.jpg)

2、**精品八股PDF**

速记八股帮助众多录友们，短时间内快速上岸：

![](https://file1.kamacoder.com/i/web/2025-09-28_17-44-23.jpg)

3、**独家资料 & 学习氛围**

大厂面经、薪资报告、秋招投递总结表

![](https://file1.kamacoder.com/i/web/2025-09-28_18-26-47.jpg)

学习路线清晰，方向明确

![](https://file1.kamacoder.com/i/web/2025-09-28_18-39-32.jpg)

星球里全是志同道合的伙伴，学习氛围 🔥🔥🔥

![](https://file1.kamacoder.com/i/web/2025-09-28_18-50-25.jpg)

4、**卡哥 1v1 提问 & 简历修改**

直接向我提问，面试疑惑、学习路线、职业规划一对一解答

![](https://file1.kamacoder.com/i/web/2025-09-29_10-07-44.jpg)

加入[知识星球](https://programmercarl.com/other/kstar.htm)后如果不满意，三天内（72h）可全额退款！




