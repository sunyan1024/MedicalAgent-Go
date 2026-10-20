#!/usr/bin/env python3
"""Summarize synthetic HTTP-boundary cache diagnostics without external packages."""
import argparse
import csv
import json
from collections import defaultdict
from pathlib import Path


def usage(row):
    attempts = row.get("attempts") or []
    frames = (attempts[-1].get("raw_usage") or []) if attempts else []
    return frames[-1] if frames else {}


def summarize(directory):
    meta = json.loads((directory / "metadata.json").read_text())
    rows = [json.loads(s) for s in (directory / "samples.jsonl").read_text().splitlines() if s.strip()]
    reasoning_mismatches = 0
    for row in rows:
        raw_reasoning = usage(row).get("completion_tokens_details", {}).get("reasoning_tokens")
        sdk_reasoning = (row.get("sdk_usage") or {}).get("completion_token_details", {}).get("reasoning_tokens", 0)
        if raw_reasoning is not None and raw_reasoning != sdk_reasoning:
            reasoning_mismatches += 1
    groups = defaultdict(list)
    flat = []
    for row in rows:
        groups[row["scenario"], row["variant"]].append(row)
        attempts = row.get("attempts") or []
        a = attempts[-1] if attempts else {}
        u = usage(row)
        flat.append(dict(scenario=row["scenario"], variant=row["variant"], step=row["step"], success=row["success"],
                         usage_match=row["usage_match"], prompt_tokens=u.get("prompt_tokens"), cached_tokens=row.get("raw_cached_tokens"),
                         request_hash=a.get("request_hash"), identical_to_previous=bool(a.get("previous_request_hash")) and a.get("request_hash")==a.get("previous_request_hash"),
                         tools_hash=a.get("tools_hash"), tools=",".join(a.get("tool_names") or []), messages_hash=a.get("messages_hash"),
                         same_leading_messages=a.get("same_leading_messages"), common_content_chars=a.get("common_content_chars"),
                         first_different_message=a.get("first_different_message"), request_file=a.get("request_file"),
                         response_id=a.get("response_id"), response_model=a.get("response_model"), duration_ms=row.get("duration_ms"),
                         finish_reason=row.get("finish_reason")))
    if flat:
        with (directory / "requests.csv").open("w", newline="") as f:
            writer = csv.DictWriter(f, fieldnames=list(flat[0]))
            writer.writeheader()
            writer.writerows(flat)
    matched = sum(r["usage_match"]=="match" for r in rows)
    mismatched = sum(r["usage_match"]=="mismatch" for r in rows)
    success = sum(r["success"] for r in rows)
    http_attempts = sum(len(r.get("attempts") or []) for r in rows)
    resolved = sorted({a["response_model"] for r in rows for a in r.get("attempts") or [] if a.get("response_model")})
    lines = ["# 缓存零命中定向诊断", "",
             f"配置模型：`{meta['model']}`；供应商主机：`{meta['provider_host']}`；thinking：`{meta['thinking']}`。",
             f"响应模型标识：`{', '.join(resolved) or '未返回'}`。开始时间：`{meta['started_at']}`。",
             f"计划 {meta['planned_requests']} 个逻辑请求，硬上限 {meta['http_limit']} 次 HTTP 尝试；请求 max_tokens={meta['max_output_tokens']}。", "",
             "## 完整性与采集校验", "",
             f"- 已记录 {len(rows)}/{meta['planned_requests']} 个逻辑请求，{http_attempts} 次 HTTP 尝试，成功 {success}。",
             f"- 原始 usage 与 SDK 的核心字段一致 {matched} 次，不一致 {mismatched} 次，其余 {len(rows)-matched-mismatched} 次缺失或不完整。",
             f"- 独立核对 reasoning_tokens，发现 {reasoning_mismatches} 次不一致。供应商独有而 SDK 未映射的字段仅保留原值。",
             "- 每次 HTTP 尝试分别保存原始 usage、请求正文文件、模型与工具配置哈希；不保存认证头和模型输出正文。",
             "- cached_tokens 缺失为未知，明确零才记为零；表中缓存占比按原始 token 总量加权，不是请求命中率。", "",
             "## 分组结果（包含各组首次请求）", "",
             "|场景|版本|成功/请求|有缓存的请求|缓存 token / 输入 token|缓存 token 占比|",
             "|---|---|---|---|---|---|"]
    for (scenario, variant), samples in sorted(groups.items()):
        valid = [r for r in samples if r.get("raw_cached_tokens") is not None and usage(r).get("prompt_tokens")]
        c = sum(r["raw_cached_tokens"] for r in valid)
        p = sum(usage(r)["prompt_tokens"] for r in valid)
        rate = f"{100*c/p:.2f}%" if p else "未知"
        lines.append(f"|{scenario}|{variant}|{sum(r['success'] for r in samples)}/{len(samples)}|{sum(r['raw_cached_tokens']>0 for r in valid)}/{len(valid)}|{c}/{p}|{rate}|")
    lines += ["", "## 每次请求的证据", "",
              "step 从 0 开始；相同消息数及公共字符数均与同组上一请求比较。公共字符数不是模型 token 数。", "",
              "|场景|版本|step|原始缓存 token|usage 校验|请求完全相同|相同前导消息|公共内容字符|工具数|finish_reason|",
              "|---|---|---|---|---|---|---|---|---|---|"]
    for r in flat:
        cached = r['cached_tokens'] if r['cached_tokens'] is not None else '未知'
        tool_count = len(r['tools'].split(',')) if r['tools'] else 0
        lines.append(f"|{r['scenario']}|{r['variant']}|{r['step']}|{cached}|{r['usage_match']}|{r['identical_to_previous']}|{r['same_leading_messages']}|{r['common_content_chars']}|{tool_count}|{r['finish_reason']}|")
    lines += ["", "## 解释边界", "",
              "- repeat 检查实际请求正文是否完全一致；隐式缓存未命中并不自动构成程序错误。",
              "- changing_memory 每次修改记忆；old_padded 在旧排列前增加长稳定系统文本。单独 calibration 的输入计数仅用于验证填充文本很长，不提供公共前缀的精确 token 数。",
              "- sequence 共六轮，前四轮保持记忆，第五轮更新，第六轮保持新记忆；历史回答固定合成，排除生成文本分叉。",
              "- tools 对照的消息和 tool_choice=none 保持一致。fixed 保留全部工具；filtered 前两次全部、第三次仅记忆工具、第四次无工具。它只隔离定义变化，不评价实际工具调用。",
              "- 输入哈希及公共消息/内容字符用于客户端核验，不代表供应商内部模板、分块与路由，也不能从字符数推算精确门槛。",
              "- 本次发送 max_tokens=128，但供应商的 completion_tokens 可能包含额外推理 token，不能视为总输出硬上限；实际计数见原始 usage。finish_reason=length 仍可验证 usage。此实验不评价回答质量或生产速度。",
              "- 总样本数少；工具配置与未命中的关联不能单独证明供应商内部原因。请求文件和 ID 可用于后续复核。", ""]
    notes = directory / "experimental_notes.md"
    if notes.exists():
        lines += ["## 本次结果分析", "", notes.read_text(), ""]
    (directory / "report.md").write_text("\n".join(lines))
    print(directory / "report.md")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path)
    summarize(parser.parse_args().directory)
