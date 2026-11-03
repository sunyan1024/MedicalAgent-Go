#!/usr/bin/env python3
"""Check the archived A/B/C inputs and call accounting without model calls."""
import hashlib
import json
from pathlib import Path
import sys

root=Path(__file__).resolve().parents[1]
p=Path(sys.argv[1])
data=json.loads((p/'dataset.json').read_text())
freeze=json.loads((root/'experiments/summary-information-v1/freeze.json').read_text())
for name,source in [('dataset.json',root/'experiments/summary-information-v1/dataset.json'),('summary_information.py',root/'scripts/summary_information.py'),('summary_information_test.go',root/'common/aihelper/summary_information_test.go')]:
    assert hashlib.sha256(source.read_bytes()).hexdigest()==freeze[name],f'changed frozen file: {name}'
assert data==json.loads((root/'experiments/summary-information-v1/dataset.json').read_text())
events=[json.loads(x) for x in (p/'events.jsonl').read_text().splitlines()]
status=json.loads((p/'status.json').read_text())
calls=[e for e in events if e['kind'] in ('answer','summary')]
answers=[e for e in events if e['kind']=='answer']
summaries=[e for e in events if e['kind']=='summary']
assert [e['call'] for e in calls]==list(range(1,len(calls)+1))
assert len(calls)==status['calls']<=70
assert len(answers)==status['answers']
checks=0
fallbacks=[]
for si,s in enumerate(data['scenarios']):
    previous_version=0
    for cp in s['checkpoints']:
        selected=[e for e in answers if e['scenario']==s['id'] and e['checkpoint']==cp['checkpoint']]
        if len(selected)!=3:
            assert not status['complete'];continue
        assert [e['group'] for e in selected]==['ABC'[(step+si+cp['checkpoint']-1)%3] for step in range(3)]
        by={e['group']:e for e in selected}
        assert by['A']['input'][-1]==by['B']['input'][-1]==by['C']['input'][-1]
        expected=s['messages'][:cp['through']]
        assert [(m['role'],m['content']) for m in by['A']['input'][:-1]]==[(m['role'],m['content']) for m in expected]
        recent=[m for m in by['C']['input'][:-1] if m['role']!='system']
        assert by['B']['input'][:-1]==recent
        assert [(m['role'],m['content']) for m in recent]==[(m['role'],m['content']) for m in expected[-len(recent):]]
        snapshot=next(e for e in events if e['kind']=='snapshot' and e['scenario']==s['id'] and e['checkpoint']==cp['checkpoint'])
        state=snapshot['state']
        assert state['Version']>=previous_version
        assert all(e['summary_version']==state['Version'] for e in selected)
        systems=[m for m in by['C']['input'][:-1] if m['role']=='system']
        assert not state['Text'] or len(systems)==1 and systems[0]['content'].endswith(state['Text'])
        if state['Through']:
            idx=next(i for i,m in enumerate(expected) if m['id']==state['Through'])
            retained_start=len(expected)-len(recent)
            assert retained_start>=idx+1
            if retained_start>idx+1:
                attempts=[e for e in summaries if e['scenario']==s['id'] and e['checkpoint']==cp['checkpoint']]
                assert attempts and state['Version']==snapshot['previous_version']
                fallbacks.append(dict(scenario=s['id'],checkpoint=cp['checkpoint'],uncovered_ids=[m['id'] for m in expected[idx+1:retained_start]],errors=[e['error'] for e in attempts]))
        previous_version=state['Version'];checks+=1
assert not status['complete'] or checks==20 and len(answers)==60
result=dict(verified_checkpoints=checks,answer_requests=len(answers),successful_answers=sum(e['success'] for e in answers),summary_calls=len(summaries),summary_timeouts=sum(e['timeout'] for e in summaries),summary_failed_calls=sum(bool(e['error']) for e in summaries),calls=len(calls),missing_usage_calls=[e['call'] for e in calls if e['input_tokens'] is None or e['output_tokens'] is None],fallbacks=fallbacks,frozen_hashes_verified=True,inputs_and_rotation_verified=True)
(p/'verification.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print(json.dumps(result,ensure_ascii=False,indent=2))
