import contextlib
import copy
import io
import json
import unittest

import summary_medical_rolling as m


class RollingValidationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data=json.loads(m.DATA.read_text())

    def validate(self,data):
        with contextlib.redirect_stdout(io.StringIO()):m.validate(data)

    def test_frozen_medical_fixture(self):
        self.validate(self.data)

    def test_reject_future_evidence(self):
        data=copy.deepcopy(self.data)
        s=data['scenarios'][0]
        s['checkpoints'][0]['fields']['glucose']['sources']=[s['messages'][-2]['id']]
        with self.assertRaises(AssertionError):self.validate(data)

    def test_reject_resurrected_old_gold(self):
        data=copy.deepcopy(self.data)
        data['scenarios'][0]['checkpoints'][4]['fields']['glucose']['value']='6.4 mmol/L'
        with self.assertRaises(AssertionError):self.validate(data)

    def test_reject_incomplete_unknown_review_scope(self):
        data=copy.deepcopy(self.data)
        data['scenarios'][0]['checkpoints'][0]['fields']['diagnosis']['review_through']=2
        with self.assertRaises(AssertionError):self.validate(data)

    def test_reject_answer_leak(self):
        data=copy.deepcopy(self.data)
        data['scenarios'][0]['checkpoints'][0]['question']+='6.4 mmol/L'
        with self.assertRaises(AssertionError):self.validate(data)


    def test_archived_inputs_are_exact_history_suffixes(self):
        path=m.DATA.parent/'run-20261103/events.jsonl'
        if not path.exists():self.skipTest('no archived live run')
        events=[json.loads(line) for line in path.read_text().splitlines()]
        scenarios={s['id']:s for s in self.data['scenarios']}
        checked=0
        for e in events:
            if e['kind']!='answer' or e['group']!='C':continue
            scenario=scenarios[e['scenario']]
            cp=scenario['checkpoints'][e['checkpoint']-1]
            recent=[v for v in e['input'][:-1] if v['role']!='system']
            expected=scenario['messages'][:cp['through']]
            suffix=expected[-len(recent):] if recent else []
            self.assertEqual([(v['role'],v['content']) for v in recent],[(v['role'],v['content']) for v in suffix])
            self.assertTrue(not recent or recent[0]['role']=='user')
            checked+=1
        self.assertGreater(checked,0)


if __name__=='__main__':unittest.main()
