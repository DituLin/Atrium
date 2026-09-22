"""Offline regression tests: no connection to a household Core or screen."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).with_name('bench-commands.sh')


class CommandBenchmarkTest(unittest.TestCase):
    def run_benchmark(self, count=1, fail=False):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            fake = root / 'atrium'
            fake.write_text('''#!/usr/bin/env python3
import json, os, sys, time
time.sleep(0.08)
failed = os.environ.get('BENCH_TEST_FAIL') == '1'
print(json.dumps({'id':'sample', 'status':'failed' if failed else 'applied',
                  'error_code':'render_failed' if failed else ''}))
sys.exit(1 if failed else 0)
''')
            fake.chmod(0o755)
            # Reproduce BSD date's literal N independently of the test host.
            date = root / 'date'
            date.write_text('#!/bin/sh\ncase "$*" in *%N*) echo 123.N;; *) echo test-date;; esac\n')
            date.chmod(0o755)
            output = root / 'result.txt'
            env = dict(os.environ, ATRIUM_ADMIN_TOKEN='offline-test-token',
                       ATRIUM_BIN=str(fake), ATRIUM_URL='https://invalid.example',
                       BENCH_TEST_FAIL='1' if fail else '0',
                       PATH=str(root)+os.pathsep+os.environ['PATH'])
            result = subprocess.run(['bash', str(SCRIPT), str(count), 'test_screen', str(output)],
                                    env=env, capture_output=True, text=True, timeout=10)
            report = output.read_text() if output.exists() else ''
            samples = Path(str(output)+'.samples.jsonl')
            rows = [json.loads(line) for line in samples.read_text().splitlines()] if samples.exists() else []
            return result, report, rows

    def test_subsecond_measurement_on_bsd_date(self):
        result, report, rows = self.run_benchmark()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotIn('P95=0ms', report)
        self.assertEqual(len(rows), 1)
        self.assertGreaterEqual(rows[0]['elapsed_ms'], 70)
        self.assertLess(rows[0]['elapsed_ms'], 5000)
        self.assertEqual(rows[0]['status'], 'applied')

    def test_failures_retained_without_successful_percentile(self):
        result, report, rows = self.run_benchmark(2, fail=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(rows), 2)
        self.assertTrue(all(r['status'] == 'failed' and r['error_code'] == 'render_failed' for r in rows))
        self.assertIn('not_applied=2', report)
        self.assertIn('applied_P95=n/a', report)

    def test_zero_count_rejected(self):
        result, _, rows = self.run_benchmark(0)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(rows, [])


if __name__ == '__main__':
    unittest.main()
