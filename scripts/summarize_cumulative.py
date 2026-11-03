#!/usr/bin/env python3
import csv,json,sys
from pathlib import Path
p=Path(sys.argv[1]);rows=[json.loads(x) for x in (p/'samples.jsonl').read_text().splitlines()];out=[]
for r in rows:
 expected=dict(person=f"人物{r['scenario']}",onset="2026-01-03",allergy="青霉素",preference="YELLOW" if r['round']>=4 else "GREEN",pending="预约",diagnosis="unknown")
 try: actual=json.loads(r.get('answer',''))
 except (ValueError,TypeError): actual={}
 if not isinstance(actual,dict):actual={}
 r['field_scores']={k:actual.get(k)==v for k,v in expected.items()}
 r['fields_correct']=sum(r['field_scores'].values());r['correct']=r['fields_correct']==6

lines=['# 累计 token 与信息保留实验','','缺失 usage 不按零计；输入与输出均包含摘要调用。评分为六字段精确匹配。','', '|会话|组|回答数|累计输入|累计输出|字段正确/总数|整题正确|','|---|---|---|---|---|---|---|']
for scenario in sorted({r['scenario'] for r in rows}):
 for g in range(3):
  selected=[r for r in rows if r['scenario']==scenario and r['group']==g];i=o=0;valid=True
  for r in selected:
   vals=[r.get('input_tokens'),r.get('output_tokens'),r.get('summary_input'),r.get('summary_output')]
   if any(v is None or v<0 for v in vals):valid=False
   else:i+=vals[0]+vals[2];o+=vals[1]+vals[3]
   out.append(dict(scenario=scenario,group='ABC'[g],round=r['round'],cumulative_input=i if valid else None,cumulative_output=o if valid else None,fields_correct=r.get('fields_correct',0)))
  lines.append(f"|{scenario}|{'ABC'[g]}|{len(selected)}|{i if valid else '不可验证'}|{o if valid else '不可验证'}|{sum(r.get('fields_correct',0) for r in selected)}/{6*len(selected)}|{sum(bool(r.get('correct')) for r in selected)}|")
lines+=['','## 逐字段精确匹配','','|会话|组|人物|时间|过敏|偏好|待办|诊断|','|---|---|---|---|---|---|---|---|']
for scenario in sorted({r['scenario'] for r in rows}):
 for g in range(3):
  selected=[r for r in rows if r['scenario']==scenario and r['group']==g]
  lines.append('|'+ '|'.join([str(scenario),'ABC'[g]]+[f"{sum(r['field_scores'][k] for r in selected)}/{len(selected)}" for k in ('person','onset','allergy','preference','pending','diagnosis')])+'|')
lines+=['','## 累计变化（含摘要）','']
for scenario in sorted({r['scenario'] for r in rows}):
 a=[r for r in out if r['scenario']==scenario and r['group']=='A']
 c=[r for r in out if r['scenario']==scenario and r['group']=='C']
 paired=[(x,y) for x,y in zip(a,c) if x['round']==y['round']]
 valid=[(x,y) for x,y in paired if all(v is not None for v in (x['cumulative_input'],x['cumulative_output'],y['cumulative_input'],y['cumulative_output']))]
 if len(valid)!=len(paired) or len(a)!=len(c):
  lines.append(f'- 会话{scenario}：样本或 usage 不完整，不能判断整个会话净节省。');continue
 if valid:
  x,y=valid[-1];base=x['cumulative_input']+x['cumulative_output'];current=y['cumulative_input']+y['cumulative_output']
  lines.append(f'- 会话{scenario}：累计输入+输出 {base} → {current} token，降幅 {100*(1-current/base):.2f}%。仅计 token 数量，不等于费用降幅。')
  crossing=next((x['round']+1 for x,y in valid if y['cumulative_input']+y['cumulative_output']<x['cumulative_input']+x['cumulative_output']),None)
  lines.append(f'  首次观察到累计 token 净节省的回放轮次：{crossing}；后续变化见 cumulative.csv。')
lines+=['','## 完整性与复现','',f"- 回答请求 {len(rows)} 次，成功 {sum(bool(r.get('success')) for r in rows)} 次。",f"- 摘要调用 {sum(r['group']==2 and r.get('summary_input')!=0 for r in rows)} 次（通过每轮摘要 usage 标记识别）。",'- 待办字段要求精确值“预约”；“待办预约”等同义表达也失分。精确匹配失败不都代表事实遗忘。','- 结果按原始 answer 重新逐字段解析；单个字段类型错误不使其他字段失分。','- 无独立预热；延迟不作为本轮收益判断。','', '![累计 token 与准确率](comparison.png)']
lines+=['','固定回放，不回灌回答。仅两份合成会话，结果不能代表用户总体；缺少缓存明细，不推算费用。']
(p/'report.md').write_text('\n'.join(lines)+'\n')
if out:
 with (p/'cumulative.csv').open('w') as f:
  w=csv.DictWriter(f,fieldnames=list(out[0]));w.writeheader();w.writerows(out)
print(p/'report.md')
