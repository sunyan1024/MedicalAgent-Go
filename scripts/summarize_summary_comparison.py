#!/usr/bin/env python3
"""Summarize real-model synthetic summary experiments; missing usage is never zero."""
import csv
import json
import sys
from collections import defaultdict
from pathlib import Path
from statistics import median


def summarize(directory):
    rows = [json.loads(line) for line in (directory / 'samples.jsonl').read_text().splitlines() if line.strip()]
    groups = defaultdict(list)
    for row in rows:
        groups[row['scenario'], row['group']].append(row)
    results = []
    for (scenario, group), samples in sorted(groups.items()):
        good = [row for row in samples if row['success']]

        def med(key):
            values = [row[key] for row in good if isinstance(row.get(key), (int, float)) and row[key] >= 0]
            return round(median(values), 2) if values else None

        total = [row['answer_ms'] + row['prep_ms'] for row in good]
        results.append(dict(scenario=scenario, group='ABC'[group], n=len(samples), success=len(good),
                            correct=sum(bool(row.get('correct')) for row in samples),
                            input_tokens=med('input_tokens'), answer_ms=med('answer_ms'),
                            prep_ms=med('prep_ms'), total_ms=round(median(total), 2) if total else None))
    if results:
        with (directory / 'summary.csv').open('w') as output:
            writer = csv.DictWriter(output, fieldnames=list(results[0]))
            writer.writeheader()
            writer.writerows(results)
    names = ['未触发', '轮数触发', '长度触发', '首次压缩及复用']
    lines = ['# 摘要压缩实验结果', '', 'A=完整历史；B=与 C 相同的近期历史（移除摘要）；C=当前机制。', '',
             '|场景|组|成功/样本|关键词全部命中|回答输入 token 中位|回答耗时中位 ms|准备耗时中位 ms|完整耗时中位 ms|',
             '|---|---|---|---|---|---|---|---|']
    def fmt(value):
        return '不可验证' if value is None else str(value)
    for row in results:
        lines.append('|'+ '|'.join([names[row['scenario']], row['group'], f"{row['success']}/{row['n']}",
                                   str(row['correct']), *(fmt(row[key]) for key in ('input_tokens', 'answer_ms', 'prep_ms', 'total_ms'))])+'|')
    lines += ['', '## 配对变化', '', '正值表示减少。仅比较同一场景、同一重复、同一轮的成功 A/C 请求。', '',
              '|场景|有效配对|回答输入 token 降幅中位|含准备开销的耗时降幅中位|', '|---|---|---|---|']
    pairs = defaultdict(dict)
    for row in rows:
        pairs[row['scenario'], row['repeat'], row['round']][row['group']] = row
    for scenario in range(4):
        token_changes, time_changes = [], []
        count = 0
        for (s, _, _), pair in pairs.items():
            if s != scenario or not all(g in pair and pair[g]['success'] for g in (0, 2)):
                continue
            count += 1
            a, c = pair[0], pair[2]
            if a.get('input_tokens') and c.get('input_tokens') is not None:
                token_changes.append(100 * (1-c['input_tokens']/a['input_tokens']))
            if a['answer_ms']:
                time_changes.append(100 * (1-(c['answer_ms']+c['prep_ms'])/a['answer_ms']))
        def pct(values):
            return f'{median(values):.2f}%' if values else '不可验证'
        lines.append(f'|{names[scenario]}|{count}|{pct(token_changes)}|{pct(time_changes)}|')
    lines += ['', '## 连续三轮：首次与复用', '', '|阶段|组|样本数|回答输入 token 中位|完整耗时中位 ms|', '|---|---|---|---|---|']
    for first in (True, False):
        for group in (0, 1, 2):
            selected = [r for r in rows if r['scenario'] == 3 and r['group'] == group and r['success'] and (r['round'] == 0) == first]
            tokens = [r['input_tokens'] for r in selected if r.get('input_tokens') is not None]
            times = [r['answer_ms'] + r['prep_ms'] for r in selected]
            lines.append('|'+ '|'.join(["首次" if first else "复用", 'ABC'[group], str(len(selected)), fmt(median(tokens) if tokens else None), fmt(median(times) if times else None)])+'|')
    for group in (0, 2):
        selected = [r for r in rows if r['scenario'] == 3 and r['group'] == group and r['success']]
        lines.append(f"- {'ABC'[group]} 组三轮回放累计完整耗时（合计 {len(selected)} 次回答）：{sum(r['answer_ms'] + r['prep_ms'] for r in selected)} ms。")
    compressed = [r for r in rows if r['group'] == 2 and r.get('summary_present')]
    lines += ['', '## 解释边界', '', f'- 检测到摘要的 C 组回答：{len(compressed)} 次。',
              '- 当前不含首正文延迟，摘要 token 未采集，不能得出累计 token 成本或回本结论。',
              '- 正确性目前是三个关键词同时命中，不能代替结构化字段评分或医学质量评价。',
              '- 各组回答不回灌历史；复用场景是固定回放。准备耗时只归入 C，A/B 上下文转换成本未单独计时。',
              '- 模型 thinking 沿用配置；输出和推理长度可能不同，耗时变化不能全部归因于输入缩短。',
              '- 未设独立预热、未测量缓存；重复文本可能影响缓存。小样本不支持生产 SLA、可靠 P95 或显著性结论。',
              '- 失败样本保留；摘要失败后的窗口截断不算成功摘要。']
    if not any(r['success'] for r in rows):
        lines += ['', '**全部回答请求失败，未获得可用于量化摘要提升的真实模型数据。**']
    (directory / 'report.md').write_text('\n'.join(lines)+'\n')
    print(directory / 'report.md')


if __name__ == '__main__':
    summarize(Path(sys.argv[1]))
