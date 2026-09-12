#!/usr/bin/env python3
"""Live HTTP comparison. No network calls unless --live is explicitly supplied."""
import argparse
import json
import os
from pathlib import Path
import re
import time
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]


def request(base, token, path, body=None, method='POST', multipart=False):
    headers = {'Authorization': 'Bearer ' + token}
    if multipart:
        boundary = 'medical-' + uuid.uuid4().hex
        data = (f'--{boundary}\r\nContent-Disposition: form-data; name="text"\r\n\r\n'
                + body + f'\r\n--{boundary}--\r\n').encode()
        headers['Content-Type'] = 'multipart/form-data; boundary=' + boundary
    elif body is not None:
        data = json.dumps(body, ensure_ascii=False).encode()
        headers['Content-Type'] = 'application/json'
    else:
        data = None
    req = urllib.request.Request(base + path, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=240) as response:
        result = json.load(response)
    if result.get('status_code') not in (None, 1000):
        raise RuntimeError(result.get('status_msg', 'API error'))
    return result


def source_hit(expected, titles):
    if not expected:
        return None
    return all(any(term in title for title in titles) for term in expected)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--live', action='store_true')
    parser.add_argument('--base-url', default='http://localhost:9090/api/v3')
    parser.add_argument('--limit', type=int, default=20)
    parser.add_argument('--output', default='outputs/medical-evaluation.json')
    args = parser.parse_args()
    cases = json.loads((ROOT / 'eval/medical/cases.json').read_text())[:max(0, args.limit)]
    if not args.live:
        print(json.dumps({'mode': 'dry-run', 'cases': len(cases), 'judge_enabled': False,
                          'instructions': '设置 MEDICAL_DEMO_TOKEN（普通测试账号），添加 --live 调用真实模型。'}, ensure_ascii=False))
        return
    token = os.environ.get('MEDICAL_DEMO_TOKEN')
    if not token:
        parser.error('MEDICAL_DEMO_TOKEN is required')
    base = args.base_url.rstrip('/')
    report_text = (ROOT / 'eval/medical/report.txt').read_text()
    indicators = json.loads((ROOT / 'eval/medical/report.json').read_text())
    results = []
    for case in cases:
        row = {'id': case['id'], 'manual_review': case['manual_review'], 'manual_pass': None}
        try:
            report_id = ''
            if case['report']:
                report = request(base, token, '/medical/reports', report_text, multipart=True)['report']
                deadline = time.monotonic() + 220
                while report['status'] == 'parsing' and time.monotonic() < deadline:
                    time.sleep(2)
                    report = request(base, token, '/medical/reports/' + report['id'], method='GET')['report']
                if report['status'] == 'parsing':
                    raise RuntimeError('report parse timeout')
                # Known synthetic fixtures are explicitly confirmed; live OCR quality
                # must be reviewed separately, not counted as an Agent success.
                row['extraction_status'] = report['status']
                row['extracted_indicators'] = json.loads(report['indicatorsJson'])
                request(base, token, '/medical/reports/' + report['id'], {'indicators': indicators}, method='PATCH')
                report_id = report['id']
            start = time.monotonic()
            turns = []
            session_id = None
            for question in [case['question']] + case['followups']:
                body = {'question': question, 'modelType': 'medical_agent', 'reportId': report_id}
                if session_id:
                    body['sessionId'] = session_id
                result = request(base, token, '/AI/chat/send' if session_id else '/AI/chat/send-new-session', body)
                session_id = result['sessionId']
                turns.append(result['run'])
            events = [event for turn in turns for event in json.loads(turn['eventsJson'])]
            answer = '\n'.join(turn['answer'] for turn in turns)
            evidence = {source['id']: source for event in events for source in event.get('sources', [])}
            cited = re.findall(r'\[(med_[A-Za-z0-9_-]+)\]', answer)
            row['agent'] = {'latency_ms': round((time.monotonic() - start) * 1000),
                            'turns': turns, 'tool_calls': sum(e['type'] in ('tool_start', 'subagent_start') for e in events),
                            'keyword_pass': all(k in answer for k in case['keywords']),
                            'citation_id_valid': bool(cited) and all(c in evidence for c in cited),
                            'expected_source_hit': source_hit(case['expected_sources'], [s['title'] for s in evidence.values()])}
            # Baseline receives the same report text and all user-provided facts.
            question = '\n'.join([case['question']] + case['followups'])
            if case['report']:
                question += '\n已确认合成报告：\n' + report_text
            baseline = request(base, token, '/AI/rag/evaluate', {'judge_enabled': False, 'cases': [
                {'id': case['id'], 'question': question, 'expected_keywords': case['keywords'], 'expected_sources': case['expected_sources']} ]})
            row['rag'] = baseline
        except Exception as exc:
            row['error'] = str(exc)
        results.append(row)
        print(case['id'], 'error' if 'error' in row else 'recorded', flush=True)
        output = ROOT / args.output
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(json.dumps({'judge_enabled': False, 'clinical_accuracy': 'not_evaluated',
                                      'note': 'Use an account with no personal knowledge files for a public-corpus comparison. Keyword/citation checks are not clinical validation.',
                                      'results': results}, ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    main()
