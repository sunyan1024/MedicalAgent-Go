import importlib.util
import json
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('information',Path(__file__).with_name('summary_information.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class InformationTests(unittest.TestCase):
    def test_dataset(self):
        m.validate(json.loads(m.DATA.read_text()))

    def test_aliases_and_stale_values(self):
        spec={'value':'2026-10-02','aliases':[]}
        self.assertTrue(m.matches('10月2日',spec))
        self.assertFalse(m.matches('10月1日',spec))
        self.assertTrue(m.matches('37.8 °C',{'value':'37.8℃','aliases':[]}))
        self.assertFalse(m.matches('38.1℃',{'value':'37.8℃','aliases':[]}))
        self.assertFalse(m.matches('6.1',{'value':'6.1 mmol/L','aliases':[]}))
        self.assertTrue(m.matches(['领取结果','完成复查'],{'value':['复查','领取复查结果']}))
        self.assertTrue(m.matches(['复查预约'],{'value':['预约复查']}))
        self.assertFalse(m.matches('本人林女士',{'value':'父亲林先生','aliases':['父亲']}))
        self.assertFalse(m.matches('父亲林先生，不是本人',{'value':'本人林女士','aliases':['本人']}))

    def test_fields_independent(self):
        fields={'a':{'value':'未提供','aliases':[]},'b':{'value':'父亲','aliases':[]}}
        scored=m.score('```json\n{"a": [], "b":"父亲"}\n```',fields)
        self.assertFalse(scored['a']['correct']);self.assertTrue(scored['b']['correct'])
        self.assertEqual(scored['a']['error'],'评分格式差异')

    def test_usage_once_and_missing_propagation(self):
        events=[dict(kind=k,scenario='s',checkpoint=cp,group=g,input_tokens=i,output_tokens=o)
                for k,cp,g,i,o in [('summary',1,None,100,10),('answer',1,'A',20,2),('answer',1,'C',5,1),('answer',2,'C',7,2)]]
        rows=m.cumulative(events,'s','C')
        self.assertEqual(rows[0]['cumulative_input'],105)
        self.assertEqual(rows[1]['cumulative_input'],112)
        self.assertEqual(m.cumulative(events,'s','A')[0]['cumulative_input'],20)
        events[0]['output_tokens']=None
        rows=m.cumulative(events,'s','C')
        self.assertIsNone(rows[0]['cumulative_output']);self.assertIsNone(rows[1]['cumulative_output'])
        self.assertEqual(rows[1]['cumulative_input'],112)

if __name__=='__main__':unittest.main()
