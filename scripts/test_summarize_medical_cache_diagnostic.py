import json
import tempfile
import unittest
from pathlib import Path

from summarize_medical_cache_diagnostic import summarize


class DiagnosticReportTest(unittest.TestCase):
    def report(self, cached, sdk_reasoning=3):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "metadata.json").write_text(json.dumps(dict(model="fixture", provider_host="synthetic.invalid", thinking=True,
                                                              started_at="fixture", planned_requests=1, http_limit=40, max_output_tokens=128)))
            raw = dict(prompt_tokens=100, completion_tokens=5, total_tokens=105, completion_tokens_details=dict(reasoning_tokens=3))
            if cached is not None:
                raw["prompt_tokens_details"] = dict(cached_tokens=cached)
            record = dict(scenario="repeat", variant="old", step=0, success=True, usage_match="match" if cached is not None else "partial",
                          raw_cached_tokens=cached, sdk_usage=dict(completion_token_details=dict(reasoning_tokens=sdk_reasoning)),
                          attempts=[dict(number=1, raw_usage=[raw])], finish_reason="stop")
            (root / "samples.jsonl").write_text(json.dumps(record)+"\n")
            summarize(root)
            return (root / "report.md").read_text(), (root / "requests.csv").read_text()

    def test_provider_and_sdk_use_different_reasoning_field_names(self):
        text, _ = self.report(64)
        self.assertIn("reasoning_tokens，发现 0 次不一致", text)
        self.assertIn("64/100|64.00%", text)
        text, _ = self.report(64, sdk_reasoning=2)
        self.assertIn("reasoning_tokens，发现 1 次不一致", text)

    def test_zero_is_measured_and_missing_is_unknown(self):
        text, csv = self.report(0)
        self.assertIn("0/100|0.00%", text)
        self.assertIn("|old|0|0|match|", text)
        text, _ = self.report(None)
        self.assertIn("|old|0|未知|partial|", text)
        self.assertIn("0/0|未知", text)


if __name__ == "__main__":
    unittest.main()
