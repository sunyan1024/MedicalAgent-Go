#!/usr/bin/env python3
"""Independent static validator and evidence report; frozen scorer is imported unchanged."""
import argparse
import hashlib
import json
from pathlib import Path
from summary_information import score, norm, cumulative, render
from build_summary_medical_rolling import tokens

ROOT=Path(__file__).resolve().parents[1]
DATA=ROOT/'experiments/summary-medical-rolling-v1/dataset.json'
SOURCES=['scripts/build_summary_medical_rolling.py','scripts/summary_medical_rolling.py','scripts/summary_information.py','common/aihelper/summary_information_test.go','common/aihelper/summary_medical_rolling_test.go','common/aihelper/context.go','common/aihelper/summary.go','common/aihelper/model.go','config/config.go']

def validate(data):
    assert data['fictional'] is True and len(data['scenarios'])==2
    ids=set()
    for s in data['scenarios']:
        contents=set()
        ms=s['messages']; index={m['id']:i+1 for i,m in enumerate(ms)}
        assert len(s['checkpoints'])==10 and len(s['probes'])>=6
        assert len(ms)%2==0
        for i,m in enumerate(ms):
            assert m['id'] not in ids and m['content'] not in contents
            ids.add(m['id']);contents.add(m['content'])
            assert m['role']==('assistant' if i%2 else 'user')
        ends=s['stage_ends'];starts=[0]+ends[:-1]
        estimates=[sum(tokens(m['content']) for m in ms[a:b]) for a,b in zip(starts,ends)]
        assert 6500<=estimates[0]<=7500 and all(n>=6000 for n in estimates[1:])
        previous=0
        factids={f['id']:f for f in s['facts']}
        for f in s['facts']:
            assert f['sources'] and all(index[src]==f['effective_from'] for src in f['sources'])
            if f['supersedes']:
                old=factids[f['supersedes']]
                assert old['key']==f['key'] and old['effective_from']<f['effective_from']
        for n,cp in enumerate(s['checkpoints'],1):
            assert cp['checkpoint']==n and previous<cp['through']<=len(ms) and cp['through']%2==0
            previous=cp['through']
            # Scoring is exactly the existing frozen function, including aliases and arrays.
            gold=json.dumps({k:v['value'] for k,v in cp['fields'].items()},ensure_ascii=False)
            assert all(r['correct'] for r in score(gold,cp['fields']).values())
            for key,f in cp['fields'].items():
                assert all(src in index and index[src]<=cp['through'] for src in f['sources'])
                vals=f['value'] if isinstance(f['value'],list) else [f['value']]
                assert all(norm(v) not in norm(cp['question']) for v in vals)
                if f.get('evidence_type')=='absence':assert f['review_through']==cp['through']
                if key!='owner':
                    applicable=[x for x in s['facts'] if x['key']==key and x['effective_from']<=cp['through']]
                    assert applicable[-1]['value']==f['value'] and applicable[-1]['sources']==f['sources']
        print(s['id'],'stage history estimates:',estimates)

def freeze(data):
    files=[str(DATA.relative_to(ROOT))]+SOURCES
    frozen={p:hashlib.sha256((ROOT/p).read_bytes()).hexdigest() for p in files}
    (DATA.parent/'freeze.json').write_text(json.dumps(frozen,indent=2)+'\n')
    lines=['# 虚构医疗事实与更正：冻结材料','', '仅用于信息保真测试，所有人物与记录均为虚构，不提供诊疗建议。', '', '原始数据、逐字段答案、来源、生效位置、替代关系、未知信息审查边界见 dataset.json。', '模拟压缩结果仅证明阶段长度具备触发条件，不能代替实际摘要覆盖。', '']
    for s in data['scenarios']:
        lines+=['## '+s['id'],'','### 事实清单','', '```json',json.dumps(s['facts'],ensure_ascii=False,indent=2),'```']
        for cp in s['checkpoints']:
            lines+=['',f"### 检查点 {cp['checkpoint']}（历史至消息 {cp['through']}）",'',cp['question'],'','```json',json.dumps(cp['fields'],ensure_ascii=False,indent=2),'```']
    (DATA.parent/'dataset.md').write_text('\n'.join(lines)+'\n')

def report(path,data):
    assert json.loads((path/'dataset.json').read_text())==data
    freeze_hashes=json.loads((path/'freeze.json').read_text())
    for filename,digest in freeze_hashes.items():
        candidates=[DATA] if filename=='dataset.json' else [ROOT/p for p in SOURCES if Path(p).name==filename]
        assert len(candidates)==1 and hashlib.sha256(candidates[0].read_bytes()).hexdigest()==digest,filename
    events=[json.loads(l) for l in (path/'events.jsonl').read_text().splitlines()]
    status=json.loads((path/'status.json').read_text())
    meta=json.loads((path/'metadata.json').read_text())
    assert meta['history_budget_estimated_tokens']==6000 and isinstance(meta['model'],str) and bool(meta['model'].strip()) and isinstance(meta['thinking'],bool)
    calls=[e for e in events if e['kind'] in ('summary','answer')]
    assert [e['call'] for e in calls]==list(range(1,len(calls)+1)) and len(calls)==status['calls']<=70
    assert sum(e['kind']=='answer' for e in calls)==status['answers']
    # Existing renderer gives strict scores, input/output cumulative curves and complete raw evidence.
    render(path,data)
    checks=[];probes=[];verified=0
    for si,s in enumerate(data['scenarios']):
        ms=s['messages'];index={m['id']:i+1 for i,m in enumerate(ms)}
        prev=0;through=0
        snapshots=[e for e in events if e['kind']=='snapshot' and e['scenario']==s['id']]
        for snap in snapshots:
            state=snap['state'];cp=s['checkpoints'][snap['checkpoint']-1]
            assert state['Version'] in (prev,prev+1)
            pos=index.get(state['Through'],0)
            assert through<=pos<=cp['through']
            if state['Version']>prev:
                assert pos>through
                decision=[e for e in events if e['kind']=='summary_decision' and e['scenario']==s['id'] and e['checkpoint']==cp['checkpoint']]
                assert len(decision)==1 and decision[0]['status']=='saved'
            prev=state['Version'];through=pos
            answers=[e for e in events if e['kind']=='answer' and e['scenario']==s['id'] and e['checkpoint']==cp['checkpoint']]
            if len(answers)!=3:continue
            assert [e['group'] for e in answers]==['ABC'[(step+si+cp['checkpoint']-1)%3] for step in range(3)]
            a,b,c=[next(e for e in answers if e['group']==g) for g in 'ABC']
            assert [(m['role'],m['content']) for m in a['input'][:-1]]==[(m['role'],m['content']) for m in ms[:cp['through']]]
            assert b['input']==[m for m in c['input'] if m['role']!='system']
            assert a['input'][-1]==b['input'][-1]==c['input'][-1]
            assert c['estimated_input_tokens']<=6000
            systems=[m for m in c['input'] if m['role']=='system']
            assert not state['Text'] or len(systems)==1 and systems[0]['content'].endswith(state['Text'])
            assert all(e['summary_version']==state['Version'] for e in answers)
            verified+=1
        reached=any(e['state']['Version']>=3 for e in snapshots)
        checks.append(dict(scenario=s['id'],reached_v3=reached,versions=[dict(checkpoint=e['checkpoint'],version=e['state']['Version'],through=e['state']['Through']) for e in snapshots]))
        for p in s['probes']:
            cp=s['checkpoints'][p['checkpoint']-1];spec=cp['fields'][p['field']]
            c=next((e for e in events if e['kind']=='answer' and e['scenario']==s['id'] and e['checkpoint']==p['checkpoint'] and e['group']=='C'),None)
            snap=next((e for e in snapshots if e['checkpoint']==p['checkpoint']),None)
            row=dict(scenario=s['id'],**p,eligible=False)
            if c and snap:
                state=snap['state'];covered=index.get(state['Through'],0)
                recent='\n'.join(m['content'] for m in c['input'][:-1] if m['role']!='system')
                summary='\n'.join(m['content'] for m in c['input'] if m['role']=='system')
                sources=[ms[index[src]-1] for src in spec['sources']]
                eligible=state['Version']>=p['minimum_version'] and all(index[src]<=covered for src in spec['sources']) and all(m['content'] not in recent for m in sources)
                scored=score(c.get('answer',''),cp['fields'])[p['field']]
                row.update(eligible=eligible,version=state['Version'],through=state['Through'],sources=sources,actual_summary=summary,actual_recent=recent,score=scored)
                # Literal presence is evidence for review, never a substitute for semantic ownership/negation review.
                vals=spec['value'] if isinstance(spec['value'],list) else [spec['value']]
                row['literal_expected_in_summary']=all(any(norm(v) in norm(summary) for v in [value]+spec['aliases']) for value in vals)
                stale=[f for f in s['facts'] if f['key']==p['field'] and f['effective_from']<min(index[src] for src in spec['sources'])]
                row['old_values_in_answer']=[f['value'] for f in stale if isinstance(f['value'],str) and norm(f['value']) in norm(c.get('answer',''))]
                row['review_status']='需人工核对人物、否定与推测边界；原评分保持不变'
            probes.append(row)
    covered=all(c['reached_v3'] for c in checks) and all(p['eligible'] for p in probes)
    result=dict(answer_plan_complete=status['complete'],rolling_coverage_complete=covered,verified_checkpoints=verified,scenarios=checks,probes=probes,missing_usage_calls=[e['call'] for e in calls if e.get('input_tokens') is None or e.get('output_tokens') is None],decisions=[e for e in events if e['kind'] in ('summary_decision','trimmed_uncovered','summary_skipped')])
    assert not status['complete'] or verified==20
    (path/'verification.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
    lines=['# 滚动摘要覆盖与独立证据','',f"60次回答计划完成：{status['complete']}；滚动更新覆盖：{'完成' if covered else '滚动更新覆盖不足'}。",'', '|会话|核验点|题号|有效摘要覆盖|严格字段正确|', '|---|---|---|---|---|']
    for p in probes:lines.append(f"|{p['scenario']}|{p['category']}|{p['checkpoint']}|{p['eligible']}|{p.get('score',{}).get('correct','未知')}|")
    lines+=['','逐项来源、实际C摘要、近期原文、旧值候选与原评分详见 verification.json。自动字面证据无法确认人物和否定，人工复核记录须单独列出，不改写 scores.json。','','## token分项与回本位置','', '|会话|回答输入A/B/C|回答输出A/B/C|全部摘要输入/输出|首次累计净节省题号|', '|---|---|---|---|---|']
    def total(rows,key):return None if any(e.get(key) is None for e in rows) else sum(e[key] for e in rows)
    for s in data['scenarios']:
        answers={g:[e for e in calls if e['kind']=='answer' and e['scenario']==s['id'] and e['group']==g] for g in 'ABC'}
        summaries=[e for e in calls if e['kind']=='summary' and e['scenario']==s['id']]
        curves={g:cumulative(events,s['id'],g) for g in 'AC'}
        breakeven=[]
        for a,c in zip(curves['A'],curves['C']):
            vs=[r[k] for r in (a,c) for k in ('cumulative_input','cumulative_output')]
            if all(v is not None for v in vs) and sum(vs[:2])>sum(vs[2:]):breakeven.append(a['checkpoint'])
        lines.append(f"|{s['id']}|{' / '.join(str(total(answers[g],'input_tokens')) for g in 'ABC')}|{' / '.join(str(total(answers[g],'output_tokens')) for g in 'ABC')}|{total(summaries,'input_tokens')} / {total(summaries,'output_tokens')}|{breakeven[0] if breakeven else '未达到或未知'}|")
    lines+=['', 'None 表示usage未知；失败调用也纳入摘要开销。逐轮累计曲线见 report.md 和 curves.svg。回本位置仅表示首次跨过零点，最终净收益仍以完整累计结果为准。', '', '收益拒绝、超时和未覆盖裁剪详见 verification.json 的 decisions，均不计作成功更新。信息保留以严格字段分数及单列人工证据为准；覆盖完成和净省token分别判断。']
    (path/'rolling-report.md').write_text('\n'.join(lines)+'\n')
    print(json.dumps({k:v for k,v in result.items() if k not in ('probes','decisions')},ensure_ascii=False,indent=2))

def main():
    parser=argparse.ArgumentParser();parser.add_argument('results',nargs='?');args=parser.parse_args()
    data=json.loads(DATA.read_text());validate(data)
    if args.results:report(Path(args.results),data)
    else:freeze(data)
if __name__=='__main__':main()
