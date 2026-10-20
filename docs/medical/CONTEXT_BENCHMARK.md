# Agent 上下文排列与性能对照

生产主 Agent 输入现在为：固定系统规则 → 完整会话历史 → 报告可用提示（若有）→ 本轮长期记忆 → 当前问题 → 本轮工具交互。每次模型调用仍在末尾添加现有预算提示。

动态记忆不写回聊天历史。普通聊天、RAG、MCP、工具定义、工具预算以及子 Agent 隔离行为不变。报告提示原本已经位于历史之后，不需要移动。无 MemoryAccess 的 Runtime 不新增记忆消息。

最新复跑：[2026-10-20 完整对照报告](benchmarks/2026-10-20-context-rerun-01/report.md)。104 次模型请求成功，12 个完整回合执行完毕；新旧版均 5/6 通过，均有 1 次证据校验失败。本轮未测出整条链路提速。

首次实验：[2026-10-20 首次对照报告](benchmarks/2026-10-20-context/report.md)。该次完整 Agent 因供应商免费额度耗尽未完成。

## 运行

使用 `config/config.toml` 的 Qwen 配置；不会输出 API key。真实测试会产生 API 用量，最多 120 次模型请求。所有历史、检索内容和记忆均为合成数据，不连接业务数据库；测试全局静音运行日志，仅保存指标。目录应为新的绝对路径，避免覆盖已有结果。

```bash
GOCACHE=/tmp/go-build-cache go test ./common/medical ./common/aihelper ./service/medical

GOCACHE=/tmp/go-build-cache \
GOPHERAI_CONTEXT_LIVE=1 \
GOPHERAI_CONTEXT_RESULTS=/tmp/medical-context-my-run \
go test ./common/medical -run '^TestMedicalContextLiveComparison$' -count=1 -v -timeout=30m

python3 scripts/summarize_medical_context.py /tmp/medical-context-my-run
```

额度恢复后只补测完整 Agent 阶段：

```bash
GOCACHE=/tmp/go-build-cache \
GOPHERAI_CONTEXT_LIVE=1 \
GOPHERAI_CONTEXT_PHASE=agent \
GOPHERAI_CONTEXT_RESULTS=/tmp/medical-context-agent-my-run \
go test ./common/medical -run '^TestMedicalContextLiveComparison$' -count=1 -v -timeout=30m

python3 scripts/summarize_medical_context.py /tmp/medical-context-agent-my-run
```

该阶段单次最多 80 次请求。旧实验结果保留；输出目录已存在 `samples.jsonl` 时拒绝覆盖。只有真实测试显式启用时才会调用模型。

若省略 `GOPHERAI_CONTEXT_RESULTS`，使用带时间戳的 `/tmp/medical-context-*` 目录。供应商不可达时，在连续三次请求错误后停止，仍可汇总部分结果。

## 比较方法

- 测试专用 `legacyContext` 从新排列还原旧排列，放回原系统提示词的相同位置。生产代码无旧版开关。
- 固定输入：三个场景（记忆不变、正文更新、仅元数据更新），每组一次预热和五次正式请求，共 36 次。约 8,000 中文字符合成历史；每次更新场景使用新版本，避免重复同一个版本掩盖收益。强制禁止工具，仅回答“好”；保留配置中的 thinking，因此输出推理长度仍有波动。
- 完整 Agent：三对两轮会话，共 12 轮。真实模型执行委派和检索，检索返回固定合成资料；第二轮模拟记忆存储更新。校验演示标识与当轮偏好码，无需持久化真实用户数据。
- 交替旧版/新版先后顺序，使用等长独立会话标识隔离历史缓存，并轮换标识归属。不声称服务端缓存已清空。
- `first_ms` 是模型请求到首个非空推理/工具/正文片段，`text_ms` 是该请求到正文片段；`visible_ms` 是整轮到用户可见 answer_delta。它们不可混用。
- 缓存字段存在性从响应流确认，缺失时存 null；usage 每次模型调用保存一次，不累加重复流片段。完整运行条目没有 token 值；按 agent_call 条目汇总。
- `tools` 统计 tool_start（检索、记忆等），不含 subagent_start；完整链路耗时受工具路径和输出长度影响。

## 产物

`metadata.json` 记录模型与测试配置；`samples.jsonl` 保留逐请求和逐运行指标；`summary.csv` 为汇总；`report.md` 为中文报告。正式延迟统计排除预热及失败请求，失败率单独展示。脚本允许部分结果汇总，必须查看完整性计数。

每场景只有五对请求，属于初步观察；缓存命中变化可直接验证，延迟变化仍受网络、推理长度和供应商调度影响。不能将该实验中的百分比直接当作生产 SLA 或成本节省。

## 缓存零命中定向诊断

`TestMedicalCacheDiagnosticLive` 仅验证缓存与采集链路，不执行自主 Agent 循环。计划 36 个合成请求，HTTP 层硬上限 40 次，连续三次供应商错误即停止。沿用配置模型与 thinking，发送 `max_tokens=128`；该参数不代表包括推理在内的总输出硬上限，以供应商 usage 为准。

```bash
GOCACHE=/tmp/go-build-cache \
GOPHERAI_CACHE_DIAGNOSTIC_LIVE=1 \
GOPHERAI_CACHE_DIAGNOSTIC_RESULTS=/tmp/medical-cache-my-run \
go test -race ./common/medical -run '^TestMedicalCacheDiagnosticLive$' -count=1 -v -timeout=15m

python3 scripts/summarize_medical_cache_diagnostic.py /tmp/medical-cache-my-run
```

输出目录必须是新目录；已有 `samples.jsonl` 时拒绝覆盖。采集合成请求正文到 `requests/*.json`，包括实际模型参数、消息、工具定义，以及原始 usage、请求 ID 和 SDK 数值；不保存认证头和模型输出正文。该采集器只存在于显式启用的测试中。

对照包含相同请求重放、新旧排列的记忆变化、旧排列前置长稳定文本、六轮连续会话（第五轮才更新记忆），以及消息不变时固定/筛选工具定义的比较。长文本校准请求也会预热该文本，因此加长前缀组首次请求不能称为冷启动。工具组两分支共用相同初始请求，后续只改变 filtered 的工具定义，以观察已有缓存是否仍被复用。

`report.md` 和 `requests.csv` 分别提供分组与逐请求结果。字段缺失保留为未知，不补零；公共前导消息数与公共内容字符数不等于供应商的精确 token 前缀。测试不会断言隐式缓存必须命中，也不会将截断或极短回复当作医学回答质量测试。

定向诊断实测：[2026-10-20 零缓存验证报告](benchmarks/2026-10-20-cache-diagnostic-01/report.md)。36 次请求通过，原始 usage 与 SDK 一致；旧排列记忆更新时零命中可复现，加长稳定前缀后可命中。具体门槛和真实 auto/forced 工具调用影响仍未确定。
