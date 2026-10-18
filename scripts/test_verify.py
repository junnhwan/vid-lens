import json
from pathlib import Path
import tempfile
import unittest

from verify import PRIVATE_TEST, frontend_results, go_results


class VerificationTests(unittest.TestCase):
    def setUp(self):
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        self.path = Path(folder.name) / "result.json"

    def events(self, events):
        self.path.write_text("".join(json.dumps(e) + "\n" for e in events), encoding="utf-8")
        return self.path

    def test_full_rejects_partial_skips_even_when_go_exits_successfully(self):
        path = self.events([{"Package": "app", "Test": "TestUnit", "Action": "pass"},
                            {"Package": "app", "Test": "TestDatabase", "Action": "skip"},
                            {"Package": "app", "Action": "pass"}])
        with self.assertRaisesRegex(RuntimeError, "Unexpected Go skips"):
            go_results(path, "full", ["app"])

    def test_unit_exclusions_are_reported_and_do_not_count_as_passes(self):
        path = self.events([{"Package": "app", "Test": "TestUnit", "Action": "pass"},
                            {"Package": "app", "Test": PRIVATE_TEST, "Action": "skip"},
                            {"Package": "app", "Test": "TestDatabase", "Action": "output", "Output": "integration: excluded by -short"},
                            {"Package": "app", "Test": "TestDatabase", "Action": "skip"},
                            {"Package": "app", "Action": "pass"}])
        report = go_results(path, "unit", ["app"])
        self.assertEqual(report["passed_top_level_tests"], 1)
        self.assertEqual(len(report["skipped"]), 2)

    def test_unit_rejects_an_unclassified_skip(self):
        path = self.events([{"Package": "app", "Test": "TestUnit", "Action": "pass"},
                            {"Package": "app", "Test": "TestUnknown", "Action": "skip"},
                            {"Package": "app", "Action": "pass"}])
        with self.assertRaisesRegex(RuntimeError, "Unexpected Go skips"):
            go_results(path, "unit", ["app"])

    def test_missing_package_and_empty_suite_cannot_pass(self):
        path = self.events([{"Package": "app", "Test": "TestUnit", "Action": "pass"},
                            {"Package": "app", "Action": "pass"}])
        with self.assertRaisesRegex(RuntimeError, "did not complete"):
            go_results(path, "full", ["app", "missing"])
        with self.assertRaisesRegex(RuntimeError, "did not execute"):
            go_results(self.events([{"Package": "app", "Action": "pass"}]), "full", ["app"])

    def test_failed_package_cannot_pass_with_other_passing_tests(self):
        path = self.events([{"Package": "app", "Test": "TestUnit", "Action": "pass"},
                            {"Package": "app", "Action": "fail"}])
        with self.assertRaisesRegex(RuntimeError, "package failed"):
            go_results(path, "full", ["app"])

    def test_started_test_without_terminal_result_cannot_pass(self):
        path = self.events([{"Package": "app", "Test": "TestUnit", "Action": "pass"},
                            {"Package": "app", "Test": "TestInterrupted", "Action": "run"},
                            {"Package": "app", "Action": "pass"}])
        with self.assertRaisesRegex(RuntimeError, "terminal result"):
            go_results(path, "full", ["app"])

    def test_frontend_pending_tests_and_empty_suite_cannot_pass(self):
        for total, passed in ((3, 2), (0, 0)):
            self.path.write_text(json.dumps({"success": True, "numTotalTests": total, "numPassedTests": passed}))
            with self.assertRaisesRegex(RuntimeError, "skipped tests"):
                frontend_results(self.path)
        self.path.write_text(json.dumps({"success": True, "numTotalTests": 3, "numPassedTests": 3, "numPendingTests": 1}))
        with self.assertRaisesRegex(RuntimeError, "skipped tests"):
            frontend_results(self.path)


if __name__ == "__main__":
    unittest.main()
