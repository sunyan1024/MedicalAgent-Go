# 医疗 Agent 长期记忆

每位用户在 MySQL `user_memories` 中拥有一篇 Markdown 文档。数据库是唯一来源，不创建实体 `.md` 文件。仅医疗 Agent 使用记忆，其他聊天模式暂不接入，也不会批量扫描历史会话提取信息。

## 使用

从菜单或聊天页的“长期记忆”进入 `/memory`，可以预览、编辑、清空文档。推荐章节：

```markdown
# 回答偏好
- 使用中文，先给结论，再解释。

# 本人健康背景
- 用户自述对青霉素过敏。

# 家属情况
- 用户自述父亲有高血压，不属于用户本人病史。
```

Agent 可自主整理用户明确表达的长期偏好与健康背景，也支持“记住”“更正”“忘记”请求。不能把检索内容、模型推测或单次报告异常直接写成长期病史。健康信息为用户自述，不替代医学证据。

## 数据流

`service/medical.Run` 绑定当前登录用户的记忆读写函数；主 Agent 每轮构造指令时从数据库加载最新文档和版本，作为不可信背景数据注入。子 Agent 仅接收任务所需背景，不直接操作记忆。

- `read_user_memory()` 返回 `contentMarkdown`、`version`、`updatedAt`。
- `update_user_memory(content_markdown, expected_version)` 保存完整文档并返回新版本和更新时间。空字符串表示清空。
- 未创建时版本为 `0`；创建后为 `1`，每次成功更新加一。更新条件同时包含 UID 和旧版本，发生冲突后必须重新读取并合并。
- 文档最多 8,000 个 Unicode 字符，超限不截断。HTTP 接口与 Agent 工具使用同一保存服务。
- 每轮记忆调用最多 3 次，独立于原有最多 3 次医学委派、合计 9 次医学工具调用。主 Agent 最多 6 次调用后总结。
- 成功保存立即生效，不依赖本轮最终回答保存成功。读取失败时允许继续回答，但应说明记忆暂不可用。

更新是全文替换：Agent 应保留无关内容，管理页遇到冲突保留草稿，加载最新内容并要求核对合并后才能重试。清空仍保留数据库行和递增版本，防止旧请求恢复旧内容。

## HTTP 接口

两条接口均要求登录且角色为普通用户；用户身份只取自 JWT，不接受请求体指定 UID。

- `GET /api/v3/memory`：直接返回 `{ "contentMarkdown": "", "version": 0, "updatedAt": "0001-01-01T00:00:00Z" }`（尚未创建时）。
- `PUT /api/v3/memory`：请求 `{ "contentMarkdown": "# 回答偏好\n简洁中文", "expectedVersion": 0 }`，成功返回更新后的文档、版本、更新时间。
- 版本冲突为 HTTP 409，参数错误为 400，存储不可用为 503；错误体含 `status_code`、`status_msg`。

后端启动时通过现有 `AutoMigrate` 创建 `user_memories`，无需旧数据回填。

## 轨迹与清空边界

记忆读写属于普通工具事件，公开轨迹只展示操作状态，不展示文档原文。原始工具输入输出仍保存在独立执行事件表中；记忆 SQL 操作关闭参数日志。

删除或清空记忆不删除原聊天、报告或执行事件。提示词要求以最新文档为准，不从旧历史自动恢复删除的信息，除非用户本轮重新明确提供或要求记住。首版不记录逐条删除历史，因此模型是否遵守该语义仍需真实模型场景验收。

## 验证

```sh
go test ./common/medical ./service/memory ./controller/memory ./service/medical ./router ./common/mysql
GOPHERAI_TEST_MYSQL=1 go test ./service/memory ./router ./service/medical ./common/mysql -count=1 -timeout 120s
cd vue-frontend
node tests/memory.test.cjs
npm run lint -- --no-fix
npm run build -- --dest /tmp/gopherai-memory-dist
```

MySQL 集成测试创建并清理随机测试数据库，不迁移应用数据库。前端状态测试使用实际组件 setup 和模拟 HTTP，不代表浏览器布局验收。

真实模型演示场景：告知偏好后换会话验证；分别提供本人和父亲病史；更正一项同时保留其他信息；在页面清空后回到包含旧消息的会话；两个页面同时编辑并处理冲突。真实模型行为与浏览器视觉效果须单独验收。
