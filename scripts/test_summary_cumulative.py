import csv,json,subprocess,sys,tempfile,unittest
from pathlib import Path

class ReportTest(unittest.TestCase):
 def test_summary_cost_and_missing_usage(self):
  with tempfile.TemporaryDirectory() as d:
   p=Path(d);rows=[]
   for g in range(3):
    rows.append(dict(scenario=0,group=g,round=0,input_tokens=100,output_tokens=10,summary_input=50 if g==2 else 0,summary_output=5 if g==2 else 0,answer=json.dumps(dict(person='人物0',onset='2026-01-03',allergy='青霉素',preference='GREEN',pending=True,diagnosis='unknown'))))
   p.joinpath('samples.jsonl').write_text('\n'.join(json.dumps(r) for r in rows))
   script=Path(__file__).with_name('summarize_cumulative.py')
   subprocess.run([sys.executable,str(script),d],check=True,capture_output=True)
   out=list(csv.DictReader(p.joinpath('cumulative.csv').read_text().splitlines()))
   self.assertEqual(out[-1]['cumulative_input'],'150');self.assertEqual(out[-1]['cumulative_output'],'15');self.assertEqual(out[-1]['fields_correct'],'5')
   rows[-1]['summary_input']=-1
   p.joinpath('samples.jsonl').write_text('\n'.join(json.dumps(r) for r in rows))
   subprocess.run([sys.executable,str(script),d],check=True,capture_output=True)
   out=list(csv.DictReader(p.joinpath('cumulative.csv').read_text().splitlines()))
   self.assertEqual(out[-1]['cumulative_input'],'')
if __name__=='__main__':unittest.main()
