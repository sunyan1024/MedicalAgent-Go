# 执行事件与 SQL 调试

`medical_run_events` 是唯一事件存储，每个事件一行。启动时自动建表并删除 `medical_runs.events` 列；不回填旧数据。现有接口的 `eventsJson` 由事件表生成，仍只展示摘要。

事件包含原始 `input`、`output`、`error`（LONGTEXT），以及 `summary`、`sources` 和 `delta`。完整原始字段不进入 SSE、历史接口和普通日志。开始事件保存输入，结束事件保存输出和错误；拒绝事件保存输入及拒绝原因。这里记录工具和委派边界的数据，不记录模型内部推理。

`sequence` 是运行内的事件顺序，`occurred_at` 为 UTC 事件时间。结束事件的 `parent_id` 指向开始事件，子工具开始事件的 `parent_id` 指向委派开始事件。委派的 `agent` 是调用者 `medical_assistant`，`tool` 是被委派的子 Agent 名称。

事件逐条独立提交，单次超时 1 秒。首次失败后暂停实时写入，结束时在总计 5 秒内幂等补写待确认事件；最终补写失败会返回事件持久化错误。进程崩溃只保留已提交事件，运行状态仍按现有逻辑在历史查询时识别为 `interrupted`。不把最终回答事务回滚扩展到已提交事件。

以下示例使用 MySQL 会话变量；替换运行 ID、用户和 UTC 时间窗口。跨运行查询通过 `medical_runs` 限定用户。

```sql
SET @run_id = '替换运行 ID';
SET @uid = '替换用户 UID';
SET @from_utc = '2026-09-14 00:00:00';
SET @until_utc = '2026-09-15 00:00:00';

-- 单轮完整事件与层级。
SELECT e.*
FROM medical_run_events e
JOIN medical_runs r ON r.id = e.run_id
WHERE e.run_id = @run_id AND r.uid = @uid
ORDER BY e.sequence;

-- 将一次调用的输入与输出、错误合并展示。
SELECT s.id, s.parent_id, s.agent, s.tool, s.input,
       f.status, f.duration_ms, f.output, f.error
FROM medical_run_events s
JOIN medical_runs r ON r.id = s.run_id
LEFT JOIN medical_run_events f
  ON f.run_id = s.run_id AND f.parent_id = s.id
 AND f.type IN ('tool_end', 'subagent_end')
WHERE s.run_id = @run_id AND r.uid = @uid
  AND s.type IN ('tool_start', 'subagent_start')
ORDER BY s.sequence;

-- 失败和拒绝分别显示；拒绝不计入已执行调用失败率。
SELECT e.run_id, e.sequence, e.agent, e.tool, e.type,
       e.status, e.error, e.occurred_at
FROM medical_run_events e
JOIN medical_runs r ON r.id = e.run_id
WHERE r.uid = @uid AND e.status IN ('failed', 'rejected')
  AND e.occurred_at >= @from_utc AND e.occurred_at < @until_utc
ORDER BY e.occurred_at, e.run_id, e.sequence;

-- 以结束事件统计已执行调用；时间窗口按结束事件发生时间。
SELECT e.agent, e.tool, COUNT(*) AS finished_calls,
       SUM(e.status = 'failed') AS failed_calls,
       ROUND(100.0 * SUM(e.status = 'failed') / COUNT(*), 2) AS failure_percent,
       ROUND(AVG(e.duration_ms), 2) AS average_duration_ms
FROM medical_run_events e
JOIN medical_runs r ON r.id = e.run_id
WHERE r.uid = @uid AND e.type = 'tool_end'
  AND e.occurred_at >= @from_utc AND e.occurred_at < @until_utc
GROUP BY e.agent, e.tool;

-- 未观察到结束事件的调用，不能直接视作工具失败。
SELECT s.run_id, s.id, s.agent, s.tool, r.status AS run_status
FROM medical_run_events s
JOIN medical_runs r ON r.id = s.run_id
WHERE r.uid = @uid
  AND s.type IN ('tool_start', 'subagent_start')
  AND s.occurred_at >= @from_utc AND s.occurred_at < @until_utc
  AND NOT EXISTS (
    SELECT 1 FROM medical_run_events f
    WHERE f.run_id = s.run_id AND f.parent_id = s.id
      AND f.type IN ('tool_end', 'subagent_end')
  );
```

工具耗时从开始事件写入返回后计时，包含工具函数执行时间；委派耗时还包含子 Agent 内部的模型、工具及事件持久化时间。平均耗时不混合委派与底层工具，避免重复计算。
