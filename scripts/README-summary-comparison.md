# 摘要压缩连续回放实验

运行前确保 `config/config.toml` 中模型凭据可用，MySQL 账户可创建临时数据库。
实验不连接业务数据库、不调用 RabbitMQ；最多 70 次模型请求，无自动重试。

```sh
SUMMARY_LIVE=1 SUMMARY_RESULTS=/tmp/summary-cumulative GOPHERAI_TEST_MYSQL=1 GOCACHE=/tmp/go-build-cache go test ./common/aihelper -run TestSummaryLiveComparison -count=1 -v -timeout 30m
python3 scripts/summarize_cumulative.py /tmp/summary-cumulative
python3 scripts/plot_summary_cumulative.py /tmp/summary-cumulative
```

绘图依赖 matplotlib；不需要修改项目依赖。每次实验使用新的结果目录。

A 为完整历史，B 为 C 近期消息去掉摘要，C 调用实际摘要构建与持久化逻辑。
两份合成会话分别从 16 轮普通历史和 12 轮超长历史开始，各回放 10 轮。
第 5 个回放问题前加入偏好更正；测评答案不回灌后续历史。
字段为人物、时间、过敏、偏好、待办、诊断，按预设值严格匹配；同义表达也可能失分，需查看原始回答。
评分器独立解析各字段，非法字段类型不影响其他字段计分。

`samples.jsonl` 保留原始测评回答和摘要 usage；`cumulative.csv` 逐轮累加全部摘要及回答 token。
摘要输入/输出为 -1 表示未获 usage，零表示该轮无摘要调用。usage 不完整时累计数据标为空，不能声明节省。
`report.md` 与图表由原始数据生成，不修改原始回答。

本实验没有独立预热，不测首正文延迟、缓存明细或实际账单。模型配置写入 metadata.json。
累计输入输出数量之和仅是 token 数量指标，不是费用指标。
只用两份会话，不提供显著性或生产性能结论。
