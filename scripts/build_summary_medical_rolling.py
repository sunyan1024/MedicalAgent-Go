#!/usr/bin/env python3
"""Build the explicitly fictional, deterministic rolling-summary fixture; no model calls."""
import json
from datetime import date, timedelta
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DEST = ROOT/'experiments/summary-medical-rolling-v1/dataset.json'

def tokens(text):
    a=sum(ord(c)<128 for c in text)
    return (a+3)//4 + 2*(len(text)-a)+8

def build(sid, cross):
    msgs=[]; facts=[]; stages=[]
    def turn(u,a):
        n=len(msgs)//2+1
        for role,content in [('user',u),('assistant',a)]:
            msgs.append(dict(id=f'{sid}-{n:03}-{role[0]}',role=role,content=content))
        return msgs[-2]['id']
    def fact(key,value,u,a,old=None,aliases=None,owner='本人',kind='provided'):
        src=turn(u,a)
        entry=dict(id=f'{key}-{len(facts)}',key=key,value=value,aliases=aliases or [],sources=[src],owner=owner,evidence_type=kind,effective_from=len(msgs)-1,supersedes=old)
        facts.append(entry);return entry
    turn('本会话所有人物、报告和记录均为虚构，仅测试信息整理；不要给出诊疗建议。我的记录主体是本人周宁，父亲叫周海。','了解，仅整理这份虚构记录；本人和父亲分开归档。')
    owner='父亲周海' if cross else '本人周宁'
    datefact=fact('report_date','2026-08-03',f'{owner}的第一份随访报告日期是2026年8月3日。','先按报告所写日期归档，后续若有勘误再更新。',owner=owner)
    glucose=fact('glucose','6.4 mmol/L',f'{owner}这份报告的空腹血糖为6.4 mmol/L。','已记下这项检验及单位；暂不解读数值。',owner=owner)
    allergy=fact('self_allergy','青霉素', '本人周宁明确提供的过敏史是青霉素过敏。','这一项属于本人提供的过敏史，不扩展到家属。')
    father=fact('father_allergy','未提供','父亲没有向我提供过敏史资料。','父亲的过敏史保持未知，不能从本人记录推断。',owner='父亲周海',kind='absence')
    hypothesis=fact('diagnosis','未提供','我猜这些记录是不是意味着糖尿病，但没有提供任何确诊结论。','这只是你的推测，当前资料没有确诊信息。',kind='absence')
    appt=fact('appointment','2026-09-12',f'{owner}的复查预约暂定2026年9月12日。','这表示预约日期，不能当作已完成的就诊日期。',owner=owner)
    status=fact('pending',['领取报告'],f'{owner}目前待办只有领取报告，复查预约已经完成登记。','登记完成和实际复查是不同状态，保留尚待领取报告这一项。',owner=owner)
    if cross:
        fact('self_report_date','2026-08-07','本人周宁的体检报告日期为2026年8月7日。','本人体检独立保存，不覆盖父亲的随访报告。')
        fact('self_glucose','5.2 mmol/L','本人周宁体检的空腹血糖是5.2 mmol/L。','这一项仅属于本人，不能挪到父亲记录中。')
    # Each additional turn supplies two distinct concrete archive facts, with a short acknowledgement.
    # No repeated medical baseline, invented diagnostic advice, or answer-bearing filler.
    sites=['东侧窗口','西侧窗口','门诊服务台','自助机旁','资料室入口','北楼大厅','南楼大厅','随访登记处']
    forms=['纸质收据','电子凭条','取件通知','归档标签','登记回执','授权副本','核对记录','预约存根']
    def grow(stage,target):
        begin=len(msgs);i=0
        while sum(tokens(m['content']) for m in msgs[begin:])<target:
            i+=1; d=date(2026,1,1)+timedelta(days=(stage*61+i)%240)
            person='父亲周海' if cross and i%2 else '本人周宁'
            turn(f'{person}的{forms[i%8]}编号为R{stage}-{i:03}，在{d.isoformat()}由{sites[(i+stage)%8]}登记。',
                 f'这条{forms[i%8]}按独立档案处理，记录主体为{person}；'+['登记不表示检查完成。','暂不据此更新医疗结论。','不替换先前检验数据。','保留原始凭条供后续核对。','缺失的报告字段保持未知。','这里只确认资料登记情况。','不把窗口信息当成报告签发处。','不推断预约是否到诊。','该凭条不证明用药情况。','未注明的事项状态暂不补充。'][i//8%10]+['','后续勘误应明确对应材料。','若补充信息再按来源修订。'][stage])
        return begin
    # Include baseline tokens in initial stage target.
    baseline=sum(tokens(m['content']) for m in msgs)
    grow(0,6900-baseline);stages.append(len(msgs))
    cp=[]
    def checkpoint(keys):
        n=len(cp)+1
        fields={}
        for k in keys:
            f=next(f for f in reversed(facts) if f['key']==k)
            fields[k]={k:v for k,v in f.items() if k in ('value','aliases','sources','evidence_type')}
            if f['evidence_type']=='absence':fields[k]['review_through']=len(msgs)
        labels={'report_date':'主记录报告日期','glucose':'主记录空腹血糖及单位','self_allergy':'本人过敏史','father_allergy':'父亲过敏史','diagnosis':'已提供的确诊结论','appointment':'主记录当前预约日期','pending':'主记录当前待办','self_report_date':'本人体检报告日期','self_glucose':'本人体检空腹血糖及单位'}
        q='按目前历史，分别给出'+ '、'.join(labels[k] for k in keys)+'。主记录属于谁也请核对。'
        fields['owner']=dict(value=owner,aliases=['父亲'] if cross else ['本人'],sources=[datefact['sources'][0]])
        cp.append(dict(checkpoint=n,through=len(msgs),question=q,fields=fields))
    checkpoint(['report_date','glucose','self_allergy','diagnosis'])
    for key in ['father_allergy','appointment','pending']:
        turn(f'请核对第{len(cp)+1}次档案整理是否保留人物分栏。',f'第{len(cp)+1}次整理继续保留独立人物栏，不把未提供的信息补齐。')
        checkpoint([key,'glucose','self_allergy'])
    start=len(msgs)
    glucose=fact('glucose','5.8 mmol/L',f'更正：{owner}第一份报告的空腹血糖抄错了，应为5.8 mmol/L，原来6.4 mmol/L作废。','以后以这次明确勘误为准，保留旧值仅用于追溯。',old=glucose['id'],owner=owner)
    datefact=fact('report_date','2026-08-04',f'另一项更正：{owner}第一份报告实际日期为2026年8月4日，之前8月3日无效。','报告日期已按勘误更新，不据此改变其他字段。',old=datefact['id'],owner=owner)
    grow(1,6200);stages.append(len(msgs))
    checkpoint(['report_date','glucose','self_allergy','diagnosis'])
    for key in ['father_allergy','appointment','pending']:
        turn(f'档案第{len(cp)+1}次核对，请继续区分推测和事实。',f'第{len(cp)+1}次核对仍只依据已提供材料，未知项维持未知。')
        checkpoint([key,'glucose','self_allergy'])
    appt=fact('appointment','2026-09-19',f'最终更正：{owner}复查预约改到2026年9月19日，原9月12日预约已取消。','当前预约以改期后的日期为准，不能继续使用取消的日期。',old=appt['id'],owner=owner)
    status=fact('pending',[],f'{owner}已经领取报告，现在没有待办事项。','将领取报告标记为完成，当前待办列表为空。',old=status['id'],owner=owner)
    grow(2,6200);stages.append(len(msgs))
    checkpoint(['appointment','pending','glucose','self_allergy','diagnosis'])
    turn('最后一次核对仍以人物各自的档案为准，未提供的项目不要推断。','会以各自资料和更正顺序核对，缺少内容继续明确标注。')
    checkpoint(['report_date','father_allergy','self_allergy']+(['self_report_date','self_glucose'] if cross else ['glucose']))
    probes=[dict(checkpoint=1,field='glucose',category='早期事实进入v1',minimum_version=1),dict(checkpoint=5,field='glucose',category='更正进入v2',minimum_version=2),dict(checkpoint=9,field='appointment',category='再次更正进入v3',minimum_version=3),dict(checkpoint=9,field='self_allergy',category='未变事实跨版本保留',minimum_version=3),dict(checkpoint=9,field='owner',category='人物归属',minimum_version=3),dict(checkpoint=9,field='diagnosis',category='事实与推测边界',minimum_version=3)]
    return dict(id=sid,messages=msgs,facts=facts,stage_ends=stages,checkpoints=cp,probes=probes)

def main():
    data=dict(title='虚构医疗事实与更正滚动摘要实验',fictional=True,normalization='复用 scripts/summary_information.py，冻结评分规则不改动',scenarios=[build('personal',False),build('family',True)])
    DEST.write_text(json.dumps(data,ensure_ascii=False,indent=2)+'\n')
    print(DEST)
if __name__=='__main__':main()
