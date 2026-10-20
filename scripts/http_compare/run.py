#!/usr/bin/env python3
"""Real TCP comparison. Python stdlib only; Go dependencies use each pinned checkout."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import statistics
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
COMMITS = {'new': '63698453bde8d658fa0897d6a345db5e866176d5',
           'old': '13368e2bc06d8345462cc63338096149473e2a5a'}

def write(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding='utf-8')
    path.chmod(0o600)

def command(args, **kw):
    return subprocess.run(args, check=True, **kw)

class Server:
    def __init__(self, variant, work, out, env):
        self.variant, self.work, self.out, self.env = variant, work, out, env
        self.p = subprocess.Popen([str(work / 'compare-server')], cwd=work, env=env,
                                  stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                  stderr=subprocess.DEVNULL, text=True, bufsize=1)
        self.info = self.read()
        self.url = self.info['url']

    def read(self):
        line = self.p.stdout.readline()
        if not line:
            raise RuntimeError(f'{self.variant}: launcher exited ({self.p.poll()}); no credentials logged')
        return json.loads(line)

    def control(self, **obj):
        self.p.stdin.write(json.dumps(obj) + '\n')
        self.p.stdin.flush()
        return self.read()

    def request(self, method, route, body=None, token=None, stream=False):
        headers = {'Content-Type': 'application/json'}
        if token:
            headers['Authorization'] = 'Bearer ' + token
        req = urllib.request.Request(self.url + route,
                  data=None if body is None else json.dumps(body, ensure_ascii=False).encode(),
                  headers=headers, method=method)
        start = time.monotonic()
        try:
            response = urllib.request.urlopen(req, timeout=180)
        except urllib.error.HTTPError as exc:
            response = exc
        with response:
            if not stream or 'text/event-stream' not in response.headers.get('Content-Type', ''):
                raw = response.read().decode()
                try:
                    value = json.loads(raw)
                except ValueError:
                    value = {'unparsed_body': raw}
                return {'status': response.status, 'body': value,
                        'duration_ms': (time.monotonic()-start)*1000}
            frames, raw, block = [], bytearray(), bytearray()
            while True:
                # readline on the actual socket preserves frame boundaries and flush timing.
                try:
                    line = response.readline()
                except (TimeoutError, OSError):
                    return {'status': response.status, 'frames': frames, 'raw': raw.decode(errors='replace'),
                            'incomplete_tail': block.decode(errors='replace'), 'error': 'TCP read interrupted or timed out',
                            'duration_ms': (time.monotonic()-start)*1000}
                if not line:
                    break
                raw.extend(line)
                block.extend(line)
                if line in (b'\n', b'\r\n'):
                    text = block.decode(); block.clear()
                    payload = '\n'.join(x[5:].lstrip(' ') for x in text.splitlines() if x.startswith('data:'))
                    try:
                        value = json.loads(payload)
                    except ValueError:
                        value = payload
                    frames.append({'ms': (time.monotonic()-start)*1000, 'data': value})
            return {'status': response.status, 'frames': frames, 'raw': raw.decode(),
                    'incomplete_tail': block.decode(), 'duration_ms': (time.monotonic()-start)*1000}

    def stop(self):
        if self.p.poll() is None:
            try:
                self.control(Op='stop')
                self.p.wait(timeout=15)
            except Exception:
                self.p.kill(); self.p.wait()


def analyse_round(s, group, turn, result, question, markers, expected, sid, before):
    prefix = f'g{group}-r{turn}'
    if 'raw' in result:
        (s.out / (prefix + '.sse')).write_text(result.pop('raw'), encoding='utf-8')
    frames = result.get('frames', [])
    events = [x['data'] for x in frames if isinstance(x['data'], dict)]
    for event in events:
        sid = event.get('sessionId') or sid
    answer = next((x.get('delta', '') for x in reversed(events) if x.get('type') == 'answer_final'), '')
    if not frames:
        sid = result.get('body', {}).get('sessionId', sid)
        answer = result.get('body', {}).get('Information', '')
    types = [x.get('type') if isinstance(x, dict) else x for x in [f['data'] for f in frames]]
    terminal = next((x.get('status') for x in reversed(events) if x.get('type') == 'run_end'), None)
    run = result.get('body', {}).get('run', {})
    checks = {'http_ok': result['status'] == 200, 'answer_present': bool(answer),
              'latest_preference': expected in answer,
              'all_early_markers': all(x in answer for x in markers),
              'run_success': terminal == 'completed' if frames else run.get('status') == 'completed'}
    if frames:
        checks['sse_order'] = bool(sid) and types[0] == 'run_start' and types[-1] == '[DONE]' and 'answer_delta' in types and 'answer_final' in types and 'run_end' in types and types.index('answer_delta') < types.index('answer_final') < types.index('run_end') < len(types)-1
        checks['sse_complete'] = not result['incomplete_tail']
    calls = sorted(set(s.out.glob('model-*-request.json')) - before)
    details = []
    for path in calls:
        body = json.loads(path.read_text()); msgs = body.get('messages', [])
        positions = [i for i, m in enumerate(msgs) if expected in str(m.get('content', ''))]
        first = str(msgs[0].get('content', '')) if msgs else ''
        # Exact user/assistant history preceding the current question must survive every call.
        details.append({'file': path.name, 'roles': [m.get('role') for m in msgs],
                        'memory_positions': positions,
                        'first_system_has_memory': expected in first,
                        'has_previous_preference': ('PREF_A' in json.dumps(msgs, ensure_ascii=False)),
                        'settings': {k:v for k,v in body.items() if k not in ('messages', 'tools')},
                        'tools_hash': hashlib.sha256(json.dumps(body.get('tools'), sort_keys=True).encode()).hexdigest()})
    checks['model_capture'] = bool(calls)
    checks['memory_placement'] = bool(details) and all(
        d['first_system_has_memory'] if s.variant == 'old' else (not d['first_system_has_memory'] and bool(d['memory_positions'])) for d in details)
    visible = next((f['ms'] for f in frames if isinstance(f['data'], dict) and f['data'].get('type') == 'answer_delta' and f['data'].get('delta')), None)
    result.update(group=group, turn=turn, variant=s.variant, session_id=sid, question=question,
                  answer=answer, output_chars=len(answer), checks=checks, model_calls=details,
                  first_event_ms=frames[0]['ms'] if frames else None, first_visible_ms=visible)
    write(s.out / (prefix + '.json'), result)
    return result, sid


def report(out, records, status, cleanup):
    validations = {}
    for row in records:
        events = [f['data'] for f in row.get('frames', []) if isinstance(f['data'], dict)]
        terminal = next((e.get('status') for e in reversed(events) if e.get('type') == 'run_end'), None)
        row['checks']['run_success'] = (terminal == 'completed' if events else row.get('body', {}).get('run', {}).get('status') == 'completed')
        expected = 'PREF_A' if row['turn'] <= 3 else 'PREF_B'
        placement = True
        for call in row['model_calls']:
            body=json.loads((out/row['variant']/call['file']).read_text())
            msgs=body.get('messages',[])
            memory=[(i,m) for i,m in enumerate(msgs) if m.get('role')=='system' and '本轮长期记忆数据（JSON）' in str(m.get('content',''))]
            call['authoritative_memory_positions']=[i for i,m in memory]
            call['authoritative_memory_has_latest']=bool(memory) and all(expected in m['content'] for i,m in memory)
            expected_position = 0 if row['variant']=='old' else 1+2*(row['turn']-1)
            call['memory_after_complete_history']=bool(memory) and memory[0][0]==expected_position
            placement &= call['authoritative_memory_has_latest'] and call['memory_after_complete_history']
        row['checks']['memory_placement']=bool(row['model_calls']) and placement
        if events:
            row['checks']['session_id_consistent']=all(e.get('sessionId')==row['session_id'] for e in events)

        row['prompt_tokens'] = row['cached_tokens'] = row['completion_tokens'] = 0
        row['usage_missing_calls'] = 0
        for call in row['model_calls']:
            meta_path = out/row['variant']/call['file'].replace('-request.json','-meta.json')
            meta = json.loads(meta_path.read_text()) if meta_path.exists() else {}
            usage = meta.get('raw_usage', [])
            if usage:
                u=usage[-1]
                row['prompt_tokens'] += u.get('prompt_tokens',0)
                row['completion_tokens'] += u.get('completion_tokens',0)
                row['cached_tokens'] += u.get('prompt_tokens_details',{}).get('cached_tokens',0)
            else:row['usage_missing_calls'] += 1
        row['cached_ratio'] = row['cached_tokens']/row['prompt_tokens'] if row['prompt_tokens'] else None
        write(out/row['variant']/f"g{row['group']}-r{row['turn']}.json",row)
    for v in COMMITS:
        validations[v] = {}
        for p in sorted((out/v).glob('g*-validation.json')):
            snap=json.loads(p.with_name(p.name.replace('-validation','-snapshot')).read_text())
            check=json.loads(p.read_text())
            check['runs_success']=len(snap['runs'] or [])==6 and all(r['status']=='completed' for r in snap['runs'])
            write(p,check);validations[v][p.name]=check
        for p in sorted((out/v).glob('g*-isolation.json')):
            iso=json.loads(p.read_text())
            validations[v][p.name]={k:x['no_target_leak'] for k,x in iso.items()}
    write(out/'validations.json',validations)
    metrics = {}
    for v in COMMITS:
        rows = [r for r in records if r['variant'] == v]
        metas = [json.loads(p.read_text()) for p in (out / v).glob('model-*-meta.json')]
        prompt = cached = 0
        for m in metas:
            usage = m.get('raw_usage', [])
            if usage:
                u = usage[-1]; prompt += u.get('prompt_tokens', 0)
                cached += u.get('prompt_tokens_details', {}).get('cached_tokens', 0)
        metrics[v] = {'rounds': len(rows), 'all_checks_passed': sum(all(r['checks'].values()) for r in rows),
            'success_rate':sum(all(r['checks'].values()) for r in rows)/len(rows) if rows else None,
            'business_completed_rate':sum(r['checks']['run_success'] for r in rows)/len(rows) if rows else None,
            'chat_total_ms':sum(r['duration_ms'] for r in rows),
            'check_pass_counts': {k: sum(r['checks'].get(k, False) for r in rows) for k in {k for r in rows for k in r['checks']}},
            'model_http_calls': len(list((out/v).glob('model-*-request.json'))), 'prompt_tokens': prompt,
            'cached_tokens': cached, 'cached_ratio': cached / prompt if prompt else None,
            'mean_visible_ms': statistics.mean([r['first_visible_ms'] for r in rows if r['first_visible_ms'] is not None]) if any(r['first_visible_ms'] is not None for r in rows) else None,
            'mean_total_ms': statistics.mean([r['duration_ms'] for r in rows]) if rows else None,
            'output_chars': sum(r['output_chars'] for r in rows),
            'cached_ratio_usage_complete': all(m.get('raw_usage') for m in metas) and len(metas)==len(list((out/v).glob('model-*-request.json'))),
            'first_system_unique_hashes':len({hashlib.sha256(str(json.loads((out/v/r['model_calls'][0]['file']).read_text())['messages'][0]['content']).encode()).hexdigest() for r in rows if r['model_calls']}),
            'completion_tokens':sum(r['completion_tokens'] for r in rows),
            'usage_missing_calls':sum(r['usage_missing_calls'] for r in rows),
            'history_and_isolation_passed':all(all(c.values()) for c in validations[v].values()) if validations[v] else None}
    paired = []
    for g in range(1,4):
        for t in range(1,7):
            pair = {r['variant']:r for r in records if r['group']==g and r['turn']==t}
            if len(pair)==2:
                a,b=pair['new'],pair['old']
                paired.append({'group':g,'turn':t,'total_ms_new_minus_old':a['duration_ms']-b['duration_ms'],
                     'visible_ms_new_minus_old':a['first_visible_ms']-b['first_visible_ms'] if a['first_visible_ms'] is not None and b['first_visible_ms'] is not None else None,
                     'model_calls_new_minus_old':len(a['model_calls'])-len(b['model_calls']),
                     'cached_tokens_new_minus_old':a['cached_tokens']-b['cached_tokens'],
                     'prompt_tokens_new_minus_old':a['prompt_tokens']-b['prompt_tokens'],
                     'first_model_tools_hash_equal': a['model_calls'][0]['tools_hash']==b['model_calls'][0]['tools_hash'] if a['model_calls'] and b['model_calls'] else None,
                     'first_model_settings_equal': a['model_calls'][0]['settings']==b['model_calls'][0]['settings'] if a['model_calls'] and b['model_calls'] else None,
                     'output_chars_new_minus_old':a['output_chars']-b['output_chars']})
    write(out/'metrics.json', metrics);write(out/'paired.json',paired)
    write(out/'status.json',{'status':status,'cleanup':cleanup})
    lines=['# 新旧版本真实 HTTP 接口对照报告', '', f'执行状态：{status}。', '',
           '|版本|提交|完成轮数|全部逐轮检查通过|模型 HTTP 次数|缓存 token 占比|平均正文延迟 ms|平均总耗时 ms|',
           '|---|---|---:|---:|---:|---:|---:|---:|']
    for v,m in metrics.items():
        lines.append(f"|{v}|{COMMITS[v]}|{m['rounds']}|{m['all_checks_passed']}|{m['model_http_calls']}|{format(m['cached_ratio'], '.2%') if m['cached_ratio'] is not None else '无数据'}|{round(m['mean_visible_ms']) if m['mean_visible_ms'] is not None else '无数据'}|{round(m['mean_total_ms']) if m['mean_total_ms'] is not None else '无数据'}|")
    lines += ['', '业务完成率：'+json.dumps({v:m['business_completed_rate'] for v,m in metrics.items()},ensure_ascii=False),
              '', '生成输出：'+json.dumps({v:{'正文字符':m['output_chars'],'completion_tokens_含推理':m['completion_tokens'],'聊天累计毫秒':round(m['chat_total_ms'])} for v,m in metrics.items()},ensure_ascii=False),
              '', '配对首个模型请求的工具配置及生成参数全部相同：'+str(bool(paired) and all(p['first_model_tools_hash_equal'] and p['first_model_settings_equal'] for p in paired))]
    failed=[f"{r['variant']} 第{r['group']}组第{r['turn']}轮："+'、'.join(k for k,v in r['checks'].items() if not v) for r in records if not all(r['checks'].values())]
    lines += ['', '首条系统消息不同哈希数：'+json.dumps({v:m['first_system_unique_hashes'] for v,m in metrics.items()},ensure_ascii=False),
              '', '各项通过次数：'+json.dumps({v:m['check_pass_counts'] for v,m in metrics.items()},ensure_ascii=False)]
    lines += ['', '未通过的逐轮检查：'+('；'.join(failed) if failed else '无'), '',
              '历史/落库/隔离整体通过：'+json.dumps({v:m['history_and_isolation_passed'] for v,m in metrics.items()},ensure_ascii=False)]
    lines += ['', '原始数据：每版目录中的逐轮 JSON、原始 SSE、模型请求与响应及 usage；metrics.json 为汇总，paired.json 为配对差值。',
        '', '验收详情见各轮 checks、各组 history/snapshot/validation，以及 isolation.json。HTTP 200、[DONE] 与业务成功和落库分别判断。',
        '', '解释边界：每版真实生成的回答保留到后续历史，历史可能分叉；仅三组六轮，小样本和供应商缓存波动不支持稳定性能结论。缓存占比按原始 usage 加权；缺失 usage 不能视为零缓存。模型 HTTP 次数包括工具追加调用。非流式请求无首次正文指标。',
        '', '此实验不包括浏览器渲染、反向代理、应用重启后的历史恢复，也未准备医学知识库或报告。',
        '', '清理结果：'+json.dumps(cleanup,ensure_ascii=False), '',
        '复跑：`python3 scripts/http_compare/run.py --output /tmp/medical-http-comparison-新的目录`。需现有 config/config.toml 中模型、MySQL、RabbitMQ、Redis 可用，并有创建和删除临时数据库权限。']
    (out/'报告.md').write_text('\n'.join(lines)+'\n')


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--summarize', type=Path);parser.add_argument('--output', type=Path, default=Path('/tmp')/('medical-http-comparison-'+time.strftime('%Y%m%d-%H%M%S')))
    args=parser.parse_args()
    if args.summarize:
        out=args.summarize.resolve()
        state=json.loads((out/'status.json').read_text())
        rows=[json.loads(p.read_text()) for v in COMMITS for p in sorted((out/v).glob('g*-r*.json'))]
        report(out,rows,state['status'],state['cleanup'])
        return 0
    out=args.output.resolve();out.mkdir(mode=0o700,parents=True,exist_ok=False)
    workroot=Path(tempfile.mkdtemp(prefix='medical-http-worktrees-'));servers=[];resources=[];records=[];cleanup={};status='未完成'
    def timeout(*_): raise TimeoutError('实验总超时 30 分钟')
    signal.signal(signal.SIGALRM,timeout);signal.signal(signal.SIGTERM,timeout);signal.alarm(1800)
    try:
        for v,commit in COMMITS.items():
            dest=out/v;dest.mkdir(mode=0o700);work=workroot/v
            command(['git','worktree','add','--detach',str(work),commit],cwd=ROOT,stdout=subprocess.DEVNULL)
            (work/'config/config.toml').write_text('')  # Singleton initially reads this, then original config is loaded in memory.
            shutil.copyfile(ROOT/'scripts/http_compare/launcher.go',work/'http_compare_launcher.go')
            command(['go','build','-o','compare-server','http_compare_launcher.go'],cwd=work,
                    env={**os.environ,'GOCACHE':'/tmp/go-build-cache'})
            suffix=secrets.token_hex(6);env={**os.environ,'COMPARE_CONFIG':str(ROOT/'config/config.toml'),
               'COMPARE_DB':'httpcmp_'+suffix+'_'+v,'COMPARE_QUEUE':'httpcmp_'+suffix+'_'+v,
               'COMPARE_OUT':str(dest),'GIN_MODE':'release'}
            resources.append((v,work,env));s=Server(v,work,dest,env);servers.append(s)
            write(dest/'config-summary.json',{'commit':commit,'model':s.info['model'],'thinking':s.info['thinking'],
                  'database':env['COMPARE_DB'],'queue':env['COMPARE_QUEUE'],'generation':'production model factory unchanged'})
        for group in range(1,4):
            uid=f'8877000000{group}';other=f'8877000010{group}';pw=secrets.token_urlsafe(24)
            tokens={};sids={};versions={}
            order=servers if group!=2 else servers[::-1]
            for s in order:
                for u in (uid,other): s.control(Op='seed',UID=u,Password=pw)
                tokens[s.variant]={}
                for u in (uid,other):
                    login=s.request('POST','/user/login',{'identifier':u,'password':pw})
                    tokens[s.variant][u]=login['body']['token']
                mem=s.request('GET','/memory',token=tokens[s.variant][uid])
                update=s.request('PUT','/memory',{'contentMarkdown':'这是软件测试偏好，当前偏好码为 PREF_A。请在软件测试回答中使用该码。','expectedVersion':mem['body']['version']},tokens[s.variant][uid])
                versions[s.variant]=update['body']['version'];write(s.out/f'g{group}-memory-A.json',update)
            markers=[f'SESSION_G{group}_EARLY_A',f'SESSION_G{group}_EARLY_B']
            for turn in range(1,7):
                expected='PREF_A' if turn<=3 else 'PREF_B'
                if turn==4:
                    for s in order:
                        update=s.request('PUT','/memory',{'contentMarkdown':'这是软件测试偏好，当前偏好码已更新为 PREF_B，替代旧偏好。请在软件测试回答中使用最新码。','expectedVersion':versions[s.variant]},tokens[s.variant][uid])
                        write(s.out/f'g{group}-memory-B.json',update)
                if turn<=2:
                    background=('测试背景：我们正在验证软件会话历史、认证隔离和消息保存。以下文字仅为合成测试资料，不包含医学问题。'*45)[:2000]
                    q=f'这是纯软件测试，无需医学检索、无需修改记忆。早期标识 {markers[turn-1]}。\n{background}\n请简短列出本会话已经提供的所有早期标识和个人记忆中的当前偏好码。'
                else:
                    q='这是纯软件会话测试，无需医学检索或修改记忆。请回顾本会话最初两轮，准确列出其中两个 SESSION_ 开头的早期标识，再列出个人记忆中的最新偏好码。简短回答。'
                assert len(q.encode())<12000
                for s in order:
                    metas=[json.loads(p.read_text()) for p in sorted(s.out.glob('model-*-meta.json'))]
                    if len(list(s.out.glob('model-*-request.json')))>=40 or (len(metas)>=3 and all(m.get('status',500)>=400 for m in metas[-3:])):
                        raise RuntimeError('达到模型 HTTP 上限或连续三次供应商错误，停止实验')
                    sid=sids.get(s.variant,'');route='/AI/chat/'+('send-stream-new-session' if not sid else ('send' if turn==6 else 'send-stream'))
                    body={'question':q,'modelType':'medical_agent'}
                    if sid:body['sessionId']=sid
                    before=set(s.out.glob('model-*-request.json'))
                    result=s.request('POST',route,body,tokens[s.variant][uid],stream=turn<6)
                    row,sid=analyse_round(s,group,turn,result,q,markers[:min(turn,2)],expected,sid,before)
                    records.append(row);sids[s.variant]=sid
                    print(f"{s.variant} group={group} turn={turn} checks={row['checks']} calls={len(row['model_calls'])}",flush=True)
            for s in order:
                sid=sids[s.variant];snapshot={}
                deadline=time.monotonic()+20
                while time.monotonic()<deadline:
                    snapshot=s.control(Op='snapshot',UID=uid,Session=sid)
                    if len(snapshot['database'] or [])==12:break
                    time.sleep(.2)
                history=s.request('POST','/AI/chat/history',{'sessionId':sid},tokens[s.variant][uid])
                write(s.out/f'g{group}-snapshot.json',snapshot);write(s.out/f'g{group}-history.json',history)
                rows=[r for r in records if r['variant']==s.variant and r['group']==group]
                expected_contents=[text for r in rows for text in (r['question'],r['answer'])]
                h=history['body'].get('history',[])
                checks={key: len(items or [])==12 and [m['content'] for m in items]==expected_contents and [m['is_user'] for m in items]==[True,False]*6 for key,items in [('database',snapshot['database']),('helper',snapshot['helper']),('history',h)]}
                checks['runs_success']=len(snapshot['runs'] or [])==6 and all(r['status']=='completed' for r in snapshot['runs'])
                # Verify each first model request contains exact preceding user/assistant history.
                checks['wire_history']=True
                for i,row in enumerate(rows):
                    if not row['model_calls']:checks['wire_history']=False;continue
                    wire=json.loads((s.out/row['model_calls'][0]['file']).read_text())['messages']
                    texts=[m.get('content') for m in wire]
                    previous=expected_contents[:i*2]
                    pos=0
                    for text in texts:
                        if pos<len(previous) and text==previous[pos]:pos+=1
                    checks['wire_history'] &= pos==len(previous)
                write(s.out/f'g{group}-validation.json',checks)
                iso={}
                for label,token in [('unauthenticated',None),('other_user',tokens[s.variant][other])]:
                    iso[label]={'memory':s.request('GET','/memory?uid='+uid,token=token),
                                'history':s.request('POST','/AI/chat/history',{'sessionId':sid},token),
                                'runs':s.request('GET','/medical/runs?sessionId='+sid,token=token)}
                    text=json.dumps(iso[label],ensure_ascii=False)
                    iso[label]['no_target_leak']=not any(x in text for x in markers+['PREF_A','PREF_B'])
                write(s.out/f'g{group}-isolation.json',iso)
        status='已完成 36 次真实聊天请求'
    except Exception as e:
        status=f'部分结果：{type(e).__name__}: {e}'
        print(status,flush=True)
    finally:
        signal.alarm(0)
        for s in servers:s.stop()
        for v,work,env in resources:
            try:
                p=command([str(work/'compare-server')],cwd=work,env={**env,'COMPARE_MODE':'cleanup'},stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True,timeout=20)
                cleanup[v]=json.loads(p.stdout)
            except Exception:cleanup[v]={'cleaned':False,'reason':'cleanup failed; resource names in config-summary.json'}
        for v in COMMITS:
            work=workroot/v
            if work.exists():
                try:command(['git','worktree','remove','--force',str(work)],cwd=ROOT,stdout=subprocess.DEVNULL)
                except Exception:cleanup[v+'-worktree']='清理失败'
        try:workroot.rmdir()
        except OSError:pass
        write(out/'fixture-summary.json',{'commits':COMMITS,'rounds_planned':36,'http_cap_per_version':40,'timeout_seconds':1800,
              'fixture_files':{name:hashlib.sha256((ROOT/'scripts/http_compare'/name).read_bytes()).hexdigest() for name in ['launcher.go','run.py']}})
        report(out,records,status,cleanup)
        print('RESULT_DIRECTORY='+str(out),flush=True)
    return 0 if status.startswith('已完成') else 1

if __name__=='__main__':raise SystemExit(main())
