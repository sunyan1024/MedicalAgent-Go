#!/usr/bin/env python3
"""Summarize synthetic Agent context A/B runs; uses only the Python stdlib."""
import argparse
import csv
import json
from collections import defaultdict
from pathlib import Path
from statistics import median

METRICS = ("first_ms", "text_ms", "visible_ms", "duration_ms", "output_tokens", "calls", "tools")


def summarize(directory):
    rows = [json.loads(line) for line in (directory / "samples.jsonl").read_text().splitlines() if line.strip()]
    meta = json.loads((directory / "metadata.json").read_text())
    groups = defaultdict(list)
    for row in rows:
        if not row["warmup"]:
            groups[(row["layer"], row["scenario"], row["variant"])].append(row)
    summary = []
    for (layer, scenario, variant), samples in sorted(groups.items()):
        good = [s for s in samples if s["success"]]
        result = dict(layer=layer, scenario=scenario, variant=variant, n=len(samples), success=len(good))
        for metric in METRICS:
            values = [s[metric] for s in good if s.get(metric) is not None]
            for stat, func in (("median", median), ("min", min), ("max", max)):
                result[f"{metric}_{stat}"] = round(func(values), 3) if values else None
        usage = [s for s in samples if s.get("cached_tokens") is not None and s.get("prompt_tokens")]
        result["cache_samples"] = len(usage)
        result["cache_rate_pct"] = round(100 * sum(s["cached_tokens"] for s in usage) / sum(s["prompt_tokens"] for s in usage), 3) if usage else None
        result["prompt_tokens_total"] = sum(s["prompt_tokens"] or 0 for s in samples)
        result["output_tokens_total"] = sum(s["output_tokens"] or 0 for s in samples)
        result["cached_tokens_total"] = sum(s["cached_tokens"] for s in usage) if usage else None
        summary.append(result)
    if summary:
        with (directory / "summary.csv").open("w", newline="") as f:
            writer = csv.DictWriter(f, fieldnames=list(summary[0]))
            writer.writeheader()
            writer.writerows(summary)
    def fmt(value):
        return "不可验证" if value is None else str(value)
    lines = ["# 医疗 Agent 上下文排列实测", "", f"模型：`{meta['model']}`；thinking：`{meta['thinking']}`；开始时间：`{meta['started_at']}`。", "",
             "仅使用合成数据。旧版由模型边界还原，工具、预算、模型参数与新版一致；固定输入测试禁止工具调用以隔离排列影响。",
             "首片段可能为推理或工具参数，不等于用户看到正文。缓存为供应商实际返回字段；缺失标为不可验证。", "",
             "## 固定输入（预热样本排除）", "",
             "|场景|版本|成功/样本|首片段中位 ms（范围）|正文首片段中位 ms|总耗时中位 ms|输出 token 中位|缓存命中率|",
             "|---|---|---|---|---|---|---|---|"]
    for s in summary:
        if s["layer"] != "fixed":
            continue
        lines.append(f"|{s['scenario']}|{s['variant']}|{s['success']}/{s['n']}|{fmt(s['first_ms_median'])} ({fmt(s['first_ms_min'])}–{fmt(s['first_ms_max'])})|{fmt(s['text_ms_median'])}|{fmt(s['duration_ms_median'])}|{fmt(s['output_tokens_median'])}|{fmt(s['cache_rate_pct'])}%|")
    lines += ["", "## 改善幅度", "", "正数为延迟下降，负数为变慢。百分比基于两组成功样本的中位数；同时报告配对延迟改善中位数。", "",
              "|场景|首片段延迟改善|配对首片段改善中位|缓存命中率变化|", "|---|---|---|---|"]
    for scenario in ("unchanged", "content_changed", "metadata_changed"):
        old = next((s for s in summary if (s['layer'], s['scenario'], s['variant']) == ('fixed', scenario, 'old')), None)
        new = next((s for s in summary if (s['layer'], s['scenario'], s['variant']) == ('fixed', scenario, 'new')), None)
        if not old or not new:
            continue
        a, b = old['first_ms_median'], new['first_ms_median']
        change = round(100 * (a-b)/a, 2) if a and b is not None else None
        cache = round(new['cache_rate_pct']-old['cache_rate_pct'], 2) if old['cache_rate_pct'] is not None and new['cache_rate_pct'] is not None else None
        pair_changes = []
        for i in range(1, 6):
            pair = {r['variant']: r for r in rows if r['layer']=='fixed' and r['scenario']==scenario and r['pair']==i and r['success']}
            if len(pair)==2 and pair['old']['first_ms'] and pair['new']['first_ms'] is not None:
                pair_changes.append(100*(pair['old']['first_ms']-pair['new']['first_ms'])/pair['old']['first_ms'])
        paired = round(median(pair_changes), 2) if pair_changes else None
        lines.append(f"|{scenario}|{fmt(change)}%|{fmt(paired)}%（{len(pair_changes)} 对）|{fmt(cache)} 个百分点|")
    lines += ["", "## 完整 Agent", "", "计划每组 3 个两轮会话；第二轮从内存存储提供更新后的记忆。每轮必须检索并回答正确的演示标识与偏好码。", "",
              "|版本|成功/轮数|首个可见回答中位 ms（范围）|总耗时中位 ms|模型调用中位|工具执行中位|", "|---|---|---|---|---|---|"]
    for s in summary:
        if s['layer']=='run':
            lines.append(f"|{s['variant']}|{s['success']}/{s['n']}|{fmt(s['visible_ms_median'])} ({fmt(s['visible_ms_min'])}–{fmt(s['visible_ms_max'])})|{fmt(s['duration_ms_median'])}|{fmt(s['calls_median'])}|{fmt(s['tools_median'])}|")
    expected_fixed = 6 * (meta.get("fixed_pairs", 5) + 1) if meta.get("fixed_pairs", 5) else 0
    actual_fixed = sum(r['layer']=='fixed' for r in rows)
    actual_runs = sum(r['layer']=='run' for r in rows)
    errors = [r for r in rows if not r['success']]
    lines += ["", "## 完整性与解释边界", "",
              f"- 固定输入记录 {actual_fixed}/{expected_fixed}（含 6 个预热）；完整运行 {actual_runs}/12；失败记录 {len(errors)}。",
              "- 服务端缓存无法清空，预热不能视为确定冷启动；两组使用等长独立标识，场景/会话间轮换标识归属。",
              "- 每场景仅 5 对正式请求，不能据此声称统计显著或生产 SLA 提升。模型推理长度、供应商负载、网络均影响延迟。",
              "- 缓存命中率分母仅包含供应商返回缓存字段的请求；总耗时包括生成，必须结合输出 token 与调用次数解释。",
              "- 完整 Agent 第二轮携带各组实际上一轮回答，属于实际链路观察，不是完全相同 token 输入的因果对照。",
              "- 工具执行数统计 tool_start（检索、记忆等），不含 subagent_start 委派事件；模型调用数另列。",
              "- 固定输入历史约 8,000 个中文字符（metadata 中 history_runes 还包含 JSON 结构）。", ""]
    notes = directory / "experimental_notes.md"
    if notes.exists():
        lines += ["", "## 本次实验说明", "", notes.read_text(), ""]
    (directory / "report.md").write_text("\n".join(lines))
    print(directory / "report.md")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    summarize(parser.parse_args().directory)
