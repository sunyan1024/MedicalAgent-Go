# 本次验证记录

日期：2026-09-11。

| 检查 | 结果 |
|---|---|
| `go test ./...` | 通过；默认跳过真实模型和可选数据库测试 |
| `go test -race ./common/medical` | 通过 |
| `GOPHERAI_TEST_MYSQL=1 go test ./service/medical ./router -count=1` | 通过；使用独立随机临时数据库并清理 |
| `GOPHERAI_MEDICAL_LIVE=1 go test ./common/aihelper -run '^TestMedicalLiveSmoke$' -count=1 -v` | 最终版本通过；约56.13秒，19条运行事件 |
| `npm run lint -- --no-fix` | 通过 |
| `npm run build` | 通过；存在依赖浏览器数据过期和 bundle 体积提示 |
| `python3 scripts/evaluate_medical.py` | dry-run 通过，20条案例，Judge关闭 |

真实模型检查只验证已配置Qwen的原生工具调用、子Agent委派和返回流程。检索工具使用合成软件测试资料，不代表真实医学检索质量。

复核过程中曾发现子Agent重复读取已有证据并耗尽预算。最终版本在每次模型生成前注入剩余预算，预算耗尽时通过模型选项禁止继续调用工具，并保留执行端硬限制；调整后真实模型验收、完整Go回归及并发检查均通过。单次验收耗时不能视为性能基准。

自动化覆盖：独立子Agent上下文、报告分析委派、检索结果不足后改写查询、纯知识问题不读报告、工具错误回填、局部与全局预算、正在执行的子Agent取消、真实回环MCP传输、参考区间比较、引用ID验证、图片OCR适配、文本读取、私有报告读取/确认权限、会话绑定和持久化追问、管理员医疗接口访问限制。

尚未执行：20条医学场景完整在线评测、真实照片/扫描PDF的识别效果评估、浏览器端人工验收和实际录屏。仓库提供可运行脚本及录屏流程，没有填入未实测的医学准确率或效果提升数字。
