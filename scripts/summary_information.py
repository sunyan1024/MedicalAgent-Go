#!/usr/bin/env python3
"""Frozen information-retention scorer, validator and archive renderer (stdlib only)."""
import argparse
import csv
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DATA = ROOT / 'experiments/summary-information-v1/dataset.json'
SET_ALIASES = {'复查预约': '预约复查', '预约复查': '预约复查', '领取结果': '领取复查结果', '复查结果仍待领取': '领取复查结果', '复查已完成': '复查', '完成复查': '复查', '本人完成体检': '本人体检', '本人体检已完成': '本人体检', '本人领取报告': '领取报告', '父亲完成复查': '父亲复查'}


def norm(x):
    x = str(x).strip().lower()
    x = re.sub(r'(?:2026年)?(\d{1,2})月(\d{1,2})日', lambda m: f'2026-{int(m[1]):02}-{int(m[2]):02}', x)
    x = x.replace('摄氏度', '℃').replace('°c', '℃')
    x = re.sub(r'[\s。，“”"：:；;]', '', x)
    return {'unknown': '未提供', '未知': '未提供'}.get(x, x)


def canonical_set(x):
    if not isinstance(x, list) or not all(isinstance(v, str) for v in x):
        return None
    return {norm(SET_ALIASES.get(v.strip(), v)) for v in x}


def matches(actual, spec):
    if isinstance(spec['value'], list):
        return canonical_set(actual) == canonical_set(spec['value'])
    return isinstance(actual, str) and norm(actual) in {norm(v) for v in [spec['value']] + spec['aliases']}


def parse_answer(text):
    text = text.strip()
    if text.startswith('```'):
        text = re.sub(r'^```(?:json)?\s*|\s*```$', '', text)
    try:
        value = json.loads(text)
        return value if isinstance(value, dict) else {}
    except (ValueError, TypeError):
        return {}


def score(text, fields):
    actual = parse_answer(text)
    result = {}
    for key, spec in fields.items():
        value = actual.get(key)
        ok = matches(value, spec)
        kind = None
        if not ok:
            s = str(value)
            if key not in actual or not isinstance(value, type(spec['value'])):
                kind = '评分格式差异'
            elif any(v in s for v in ('38.1', '7.1', '2026-10-01', '10月1日', '2026-10-10', '2026-10-14')):
                kind = '旧值'
            elif norm(spec['value']) == '未提供' and norm(s) != '未提供':
                kind = '未知信息编造'
            elif key == 'diagnosis':
                kind = '推测确诊'
            elif key in ('owner', 'physical_report', 'review_report', 'self_allergy', 'self_date', 'father_date', 'self_appointment', 'father_appointment', 'self_pending', 'father_pending'):
                kind = '错误人物'
            else:
                kind = '其他事实差异'
        result[key] = dict(correct=ok, actual=value, expected=spec['value'], error=kind)
    return result


def cumulative(events, scenario, group):
    """Each summary call belongs to C once, regardless of request order."""
    i = o = 0
    rows = []
    for cp in range(1, 11):
        selected = [e for e in events if e['scenario'] == scenario and e['checkpoint'] == cp and
                    (e['kind'] == 'answer' and e['group'] == group or e['kind'] == 'summary' and group == 'C')]
        if not any(e['kind'] == 'answer' for e in selected):
            break
        for e in selected:
            for key in ('input_tokens', 'output_tokens'):
                v = e.get(key)
                if v is not None and (not isinstance(v, int) or v < 0):
                    raise ValueError('invalid usage')
            i = None if i is None or e.get('input_tokens') is None else i + e['input_tokens']
            o = None if o is None or e.get('output_tokens') is None else o + e['output_tokens']
        rows.append(dict(scenario=scenario, group=group, checkpoint=cp, cumulative_input=i, cumulative_output=o))
    return rows


def validate(data):
    assert len(data['scenarios']) == 2
    for s in data['scenarios']:
        messages = s['messages']
        assert len(messages) == 52 and len(s['checkpoints']) == 10
        assert len({m['content'] for m in messages}) == len(messages), 'duplicate filler'
        assert max(len(m['content']) for m in messages) < 250
        index = {m['id']: i + 1 for i, m in enumerate(messages)}
        for cp in s['checkpoints']:
            assert cp['through'] == 32 + 2 * cp['checkpoint']
            for spec in cp['fields'].values():
                assert all(index[src] <= cp['through'] for src in spec['sources']), 'future source'
                vals = spec['value'] if isinstance(spec['value'], list) else [spec['value']]
                assert all(norm(v) not in norm(cp['question']) for v in vals), 'answer leaked in question'
                if spec.get('evidence_type') == 'absence':
                    assert spec['review_through'] == cp['through']
        # Simulate user boundaries using production thresholds; Go test verifies actual calls.
        start = calls = 0
        for cp in s['checkpoints']:
            if cp['through']//2-start > 16:
                start = cp['through']//2-11
                calls += 1
        assert calls == 2


def readable(data):
    lines = ['# 冻结的数据集与评分规则', '', '所有日期年份固定为2026；前16轮为初始历史，此后每轮交流后独立测试。测评问题与回答不回灌历史。', '',
             '评分按每题独立字段进行。JSON代码围栏可接受，单个字段格式错误不影响其他字段。数组按无序集合比较。禁止用子串匹配放过错误数值或人物。', '', json.dumps(data['normalization'], ensure_ascii=False, indent=2), '',
             '未知信息以截至检查点的全部历史为依据；来源消息仅标识当时的材料边界，不能以单条消息证明全局未提供。', '',
             '自动错误分类为初筛；摘要遗漏与回答未使用需结合完整输入、摘要和来源复核。没有相应证据时标为待复核，不强行归因。']
    for s in data['scenarios']:
        lines += ['', '## ' + s['id']]
        for n in range(26):
            lines += ['', f'### 第{n+1}轮' + ('（初始历史）' if n < 16 else f'（检查点{n-15}前新增）')]
            for m in s['messages'][2*n:2*n+2]:
                lines.append(f"- {m['id']} {m['role']}：{m['content']}")
            if n >= 16:
                cp = s['checkpoints'][n-16]
                lines += ['', '**问题**：' + cp['question'], '', '```json', json.dumps(cp['fields'], ensure_ascii=False, indent=2), '```']
    return '\n'.join(lines) + '\n'


def render(path, data):
    events = [json.loads(x) for x in (path/'events.jsonl').read_text().splitlines()]
    status = json.loads((path/'status.json').read_text())
    rows, scored = [], []
    lines = ['# 信息性多轮对话摘要压缩实验', '', f"运行完整：{status['complete']}；模型调用 {status['calls']} 次，回答 {status['answers']} 次。", '',
             'usage 缺失向后传播为不可验证；C累计包含每次摘要生成、更新与失败调用，按独立事件只计一次。', '',
             '|会话|组|字段正确/总数|整题正确/回答数|累计输入|累计输出|', '|---|---|---|---|---|---|']
    for s in data['scenarios']:
        for g in 'ABC':
            cr = cumulative(events, s['id'], g); rows += cr
            answers = [e for e in events if e['kind'] == 'answer' and e['scenario'] == s['id'] and e['group'] == g]
            correct = total = full = 0
            for e in answers:
                cp = s['checkpoints'][e['checkpoint']-1]
                fs = score(e.get('answer', ''), cp['fields'])
                correct += sum(v['correct'] for v in fs.values()); total += len(fs); full += all(v['correct'] for v in fs.values())
                scored.append(dict(scenario=s['id'], checkpoint=e['checkpoint'], group=g, scores=fs, answer=e.get('answer', '（请求失败）')))
            last = cr[-1] if cr else {}
            lines.append(f"|{s['id']}|{g}|{correct}/{total}|{full}/{len(answers)}|{last.get('cumulative_input')}|{last.get('cumulative_output')}|")
    lines += ['', '## 逐题对照与错误归因', '']
    for s in data['scenarios']:
        for cp in s['checkpoints']:
            lines += [f"### {s['id']} / {cp['checkpoint']}：{cp['question']}", '', '标准答案及来源：`' + json.dumps(cp['fields'], ensure_ascii=False) + '`', '']
            for x in [v for v in scored if v['scenario'] == s['id'] and v['checkpoint'] == cp['checkpoint']]:
                lines += [f"**{x['group']}**", '', '```text', x['answer'], '```', '', json.dumps(x['scores'], ensure_ascii=False), '']
                if not all(v['correct'] for v in x['scores'].values()):
                    event = next(e for e in events if e['kind']=='answer' and e['scenario']==s['id'] and e['checkpoint']==cp['checkpoint'] and e['group']==x['group'])
                    summary = '\n'.join(m['content'] for m in event['input'] if m['role']=='system')
                    recent = '\n'.join(m['content'] for m in event['input'] if m['role']!='system')
                    for key, v in x['scores'].items():
                        if v['correct']: continue
                        spec=cp['fields'][key]; targets=spec['value'] if isinstance(spec['value'],list) else [spec['value']]
                        evidence=all(any(norm(a) in norm(summary) for a in [target]+spec.get('aliases',[])) for target in targets)
                        in_recent=all(norm(target) in norm(recent) for target in targets)
                        cause='评分格式差异，需核对正文' if v['error']=='评分格式差异' else ('回答未使用（候选，需核对否定与人物）' if evidence or in_recent else '摘要遗漏（候选，需结合来源复核）' if x['group']=='C' else '待复核：检查可见输入与来源')
                        lines += [f'- {key}：{cause}。']
            lines.append('')
    lines += ['## 分项准确率', '', '|会话|组|题号/字段|正确/总数|', '|---|---|---|---|']
    for x in scored:
        for key,v in x['scores'].items():
            lines.append(f"|{x['scenario']}|{x['group']}|{x['checkpoint']}/{key}|{int(v['correct'])}/1|")
    lines += ['', '## 摘要调用与状态', '']
    for e in events:
        if e['kind'] in ('summary','snapshot'):
            lines += ['```json', json.dumps(e,ensure_ascii=False,indent=2), '```', '']
    lines += ['## token 与质量分别解读', '']
    for s in data['scenarios']:
        final={g:next((r for r in reversed(rows) if r['scenario']==s['id'] and r['group']==g),{}) for g in 'AC'}
        vals=[final[g].get(k) for g in 'AC' for k in ('cumulative_input','cumulative_output')]
        if status['complete'] and all(v is not None for v in vals) and sum(vals[:2]):
            lines += [f"- {s['id']}：A合计{sum(vals[:2])} token，C合计{sum(vals[2:])} token，变化{(sum(vals[2:])/sum(vals[:2])-1)*100:+.2f}%。"]
        else: lines += [f"- {s['id']}：运行或usage不完整，不能声明整个会话节省。"]
    lines += ['', '质量变化见上表及逐题对照；自动错误归因需人工复核，不能把评分失败直接等同于摘要丢失。两份虚构会话仅为探索性样本，不评价诊断与治疗建议，不推算费用，不代表生产总体表现。', '', '![累计输入与输出](curves.svg)']
    (path/'report.md').write_text('\n'.join(lines)+'\n')
    (path/'scores.json').write_text(json.dumps(scored,ensure_ascii=False,indent=2)+'\n')
    with (path/'cumulative.csv').open('w') as f:
        w=csv.DictWriter(f,fieldnames=['scenario','group','checkpoint','cumulative_input','cumulative_output']);w.writeheader();w.writerows(rows)
    svg=['<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="680" viewBox="0 0 1000 680"><rect width="100%" height="100%" fill="white"/>']
    for si,s in enumerate(data['scenarios']):
        for ki,key in enumerate(('cumulative_input','cumulative_output')):
            x0=65+ki*500;y0=45+si*330
            selected=[r for r in rows if r['scenario']==s['id'] and r[key] is not None]
            ymax=max([r[key] for r in selected]+[1])
            svg += [f'<text x="{x0}" y="{y0}" font-size="15">{s["id"]} {key} (max {ymax})</text>',f'<path d="M{x0} {y0+10} V{y0+260} H{x0+390}" fill="none" stroke="black"/>']
            for g,color in zip('ABC',['#1464c4','#e68613','#16813b']):
                points=' '.join(f'{x0+(r["checkpoint"]-1)*42},{y0+260-r[key]/ymax*240}' for r in selected if r['group']==g)
                svg += [f'<polyline points="{points}" fill="none" stroke="{color}" stroke-width="2"/>',f'<text x="{x0+80*"ABC".index(g)}" y="{y0+285}" fill="{color}">{g}</text>']
            svg += [f'<text x="{x0+280}" y="{y0+285}">checkpoint 1–10</text>']
    (path/'curves.svg').write_text(''.join(svg)+'</svg>')


def main():
    parser=argparse.ArgumentParser();parser.add_argument('results',nargs='?');args=parser.parse_args()
    data=json.loads(DATA.read_text());validate(data)
    if args.results:
        path=Path(args.results)
        assert json.loads((path/'dataset.json').read_text())==data, 'dataset changed after freeze'
        render(path,data)
    else:
        (DATA.parent/'dataset.md').write_text(readable(data))
        frozen={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in (DATA,Path(__file__),ROOT/'common/aihelper/summary_information_test.go')}
        (DATA.parent/'freeze.json').write_text(json.dumps(frozen,indent=2)+'\n')
        print('validated and frozen: 52 exchanges, 20 questions, expected 4 summary calls + 60 answers')

if __name__=='__main__':main()
