#!/usr/bin/env python3
"""Render the unchanged scorer, then report answer and summary usage separately."""
import csv
import json
import sys
from pathlib import Path

from summary_information import render, validate


def total(events, key):
    values = [event.get(key) for event in events]
    return None if any(value is None for value in values) else sum(values)


def report(path):
    data = json.loads((path / 'dataset.json').read_text())
    validate(data)
    render(path, data)
    events = [json.loads(line) for line in (path / 'events.jsonl').read_text().splitlines()]
    rows = []
    for scenario in data['scenarios']:
        summaries = [e for e in events if e['kind'] == 'summary' and e['scenario'] == scenario['id']]
        for group in 'ABC':
            answers = [e for e in events if e['kind'] == 'answer' and e['scenario'] == scenario['id'] and e['group'] == group]
            charged = summaries if group == 'C' else []
            rows.append(dict(scenario=scenario['id'], group=group,
                             answer_requests=len(answers), summary_calls=len(charged),
                             answer_input=total(answers, 'input_tokens'),
                             answer_output=total(answers, 'output_tokens'),
                             summary_input=total(charged, 'input_tokens'),
                             summary_output=total(charged, 'output_tokens')))
    with (path / 'usage_breakdown.csv').open('w') as stream:
        writer = csv.DictWriter(stream, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)
    metadata = json.loads((path / 'metadata.json').read_text())
    lines = ['# 预算与usage分项', '',
             f"Helper历史预算：{metadata['history_budget_estimated_tokens']} 估算token；首检查点：{metadata['first_checkpoint_estimates']}。",
             '回答及全部摘要调用分别计账；空白usage表示未知，不能按零计算。累计曲线见 curves.svg，原评分及格式干扰说明见 report.md。', '',
             '|会话|组|回答输入|回答输出|摘要调用|摘要输入|摘要输出|',
             '|---|---|---|---|---|---|---|']
    for row in rows:
        lines.append('|' + '|'.join(str(row[key]) for key in
                     ('scenario', 'group', 'answer_input', 'answer_output',
                      'summary_calls', 'summary_input', 'summary_output')) + '|')
    lines += ['', '## 摘要验收与裁剪', '']
    for event in events:
        if event['kind'] in ('summary_decision', 'trimmed_uncovered', 'split'):
            lines.append('- ' + json.dumps(event, ensure_ascii=False))
    (path / 'budget_report.md').write_text('\n'.join(lines) + '\n')


if __name__ == '__main__':
    report(Path(sys.argv[1]))
