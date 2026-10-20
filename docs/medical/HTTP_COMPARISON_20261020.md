# 新旧版本真实 HTTP 接口对照报告

执行状态：已完成 36 次真实聊天请求。

|版本|提交|完成轮数|全部逐轮检查通过|模型 HTTP 次数|缓存 token 占比|平均正文延迟 ms|平均总耗时 ms|
|---|---|---:|---:|---:|---:|---:|---:|
|new|63698453bde8d658fa0897d6a345db5e866176d5|18|18|27|50.11%|16290|17295|
|old|13368e2bc06d8345462cc63338096149473e2a5a|18|15|26|26.25%|22803|25386|

业务完成率：{"new": 1.0, "old": 1.0}

生成输出：{"new": {"正文字符": 1833, "completion_tokens_含推理": 22678, "聊天累计毫秒": 311303}, "old": {"正文字符": 1809, "completion_tokens_含推理": 34704, "聊天累计毫秒": 456952}}

配对首个模型请求的工具配置及生成参数全部相同：True

首条系统消息不同哈希数：{"new": 1, "old": 6}

各项通过次数：{"new": {"http_ok": 18, "model_capture": 18, "answer_present": 18, "run_success": 18, "sse_order": 15, "session_id_consistent": 15, "all_early_markers": 18, "sse_complete": 15, "latest_preference": 18, "memory_placement": 18}, "old": {"http_ok": 18, "model_capture": 18, "answer_present": 18, "run_success": 18, "sse_order": 15, "session_id_consistent": 15, "all_early_markers": 16, "sse_complete": 15, "latest_preference": 17, "memory_placement": 18}}

未通过的逐轮检查：old 第1组第2轮：all_early_markers；old 第1组第4轮：latest_preference；old 第2组第2轮：all_early_markers

历史/落库/隔离整体通过：{"new": true, "old": true}

原始数据：每版目录中的逐轮 JSON、原始 SSE、模型请求与响应及 usage；metrics.json 为汇总，paired.json 为配对差值。

验收详情见各轮 checks、各组 history/snapshot/validation，以及 isolation.json。HTTP 200、[DONE] 与业务成功和落库分别判断。

解释边界：每版真实生成的回答保留到后续历史，历史可能分叉；仅三组六轮，小样本和供应商缓存波动不支持稳定性能结论。缓存占比按原始 usage 加权；缺失 usage 不能视为零缓存。模型 HTTP 次数包括工具追加调用。非流式请求无首次正文指标。

此实验不包括浏览器渲染、反向代理、应用重启后的历史恢复，也未准备医学知识库或报告。

清理结果：{"new": {"cleaned": true}, "old": {"cleaned": true}}

复跑：`python3 scripts/http_compare/run.py --output /tmp/medical-http-comparison-新的目录`。需现有 config/config.toml 中模型、MySQL、RabbitMQ、Redis 可用，并有创建和删除临时数据库权限。

完整原始证据保存在 [benchmarks/http-comparison-20261020](benchmarks/http-comparison-20261020/)。复跑说明见 [测试夹具说明](../../scripts/http_compare/README.md)。该 benchmarks 目录按项目现有规则不纳入 Git，文件已保存到本地项目目录。
