import json
from pathlib import Path
import tempfile
import unittest

from run import COMMITS, report, write


class ReportTests(unittest.TestCase):
    def fixture(self, out, stale=False):
        rows = []
        for variant in COMMITS:
            folder = out / variant
            folder.mkdir()
            memory = '本轮长期记忆数据（JSON）：' + ('PREF_A' if stale and variant == 'new' else 'PREF_B')
            history = [{'role': 'user' if i % 2 == 0 else 'assistant', 'content': 'PREF_B'} for i in range(6)]
            messages = ([{'role': 'system', 'content': 'static'}] + history + [{'role': 'system', 'content': memory}]
                        if variant == 'new' else [{'role': 'system', 'content': 'static\n' + memory}] + history)
            messages.append({'role': 'user', 'content': 'question'})
            write(folder / 'model-001-request.json', {'messages': messages})
            write(folder / 'model-001-meta.json', {'raw_usage': [{'prompt_tokens': 100,
                  'completion_tokens': 20, 'prompt_tokens_details': {'cached_tokens': 30}}]})
            events = [{'type': 'run_start'}, {'type': 'answer_delta', 'delta': 'PREF_B'},
                      {'type': 'answer_final', 'delta': 'PREF_B'}, {'type': 'run_end', 'status': 'completed'}]
            rows.append({'variant': variant, 'group': 1, 'turn': 4, 'session_id': 'session',
                         'checks': {'run_success': False}, 'model_calls': [{'file': 'model-001-request.json',
                         'tools_hash': 'same', 'settings': {}}], 'output_chars': 6,
                         'frames': [{'data': {**e, 'sessionId': 'session'}, 'ms': i} for i, e in enumerate(events)],
                         'duration_ms': 100, 'first_visible_ms': 10})
        return rows

    def test_completed_status_and_raw_cache_usage(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory)
            rows = self.fixture(out)
            report(out, rows, 'test only', {})
            metrics = json.loads((out / 'metrics.json').read_text())
            for variant in COMMITS:
                self.assertEqual(metrics[variant]['success_rate'], 1)
                self.assertEqual(metrics[variant]['cached_ratio'], .3)
                self.assertEqual(metrics[variant]['completion_tokens'], 20)

    def test_stale_memory_cannot_pass_using_current_code_in_history(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory)
            rows = self.fixture(out, stale=True)
            report(out, rows, 'test only', {})
            new = next(row for row in rows if row['variant'] == 'new')
            self.assertFalse(new['checks']['memory_placement'])
            self.assertTrue(new['checks']['run_success'])


if __name__ == '__main__':
    unittest.main()
