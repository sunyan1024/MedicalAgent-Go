import unittest

from report_summary_budget import total


class BudgetAccountingTests(unittest.TestCase):
    def test_missing_usage_is_unknown_and_no_call_is_zero(self):
        self.assertEqual(total([], 'input_tokens'), 0)
        self.assertEqual(total([{'input_tokens': 12}, {'input_tokens': 8}], 'input_tokens'), 20)
        self.assertIsNone(total([{'input_tokens': 12}, {'input_tokens': None}], 'input_tokens'))
        self.assertIsNone(total([{}], 'input_tokens'))


if __name__ == '__main__':
    unittest.main()
