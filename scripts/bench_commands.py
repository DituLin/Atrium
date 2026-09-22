"""Measure command CLI round trips with a monotonic clock on macOS/Linux.

This is an upper bound including CLI startup, request and 250 ms polling, not
server timestamp subtraction. Persist every attempt; never retry a command.
"""
import argparse
from datetime import datetime, timezone
import json
import math
import os
from pathlib import Path
import subprocess
import time


def positive(value):
    number = int(value)
    if number <= 0:
        raise argparse.ArgumentTypeError('count must be positive')
    return number


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('count', nargs='?', type=positive, default=100)
    parser.add_argument('screen', nargs='?', default='living_room_tv')
    parser.add_argument('outfile', nargs='?', default='bench-commands-'+datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')+'.txt')
    args = parser.parse_args()
    output = Path(args.outfile)
    samples = Path(str(output)+'.samples.jsonl')
    header = ('# Atrium screen control benchmark\n'
              f'# date: {datetime.now(timezone.utc).isoformat()}\n'
              f'# screen: {args.screen}\n'
              '# metric: CLI start to terminal receipt; monotonic milliseconds\n'
              '# Includes startup, request and 250 ms polling; conservative upper bound.\n'
              '# A result above target alone cannot establish actual API-to-applied latency.\n'
              '# target: P95 <= 1000 ms, max <= 3000 ms, all applied\n')
    times = []
    failures = 0
    # Do not overwrite raw evidence from a prior run.
    with output.open('x') as summary, samples.open('x') as raw:
        summary.write(header)
        summary.flush()
        for index in range(1, args.count+1):
            route = 'dashboard' if index % 2 == 0 else 'photos'
            command = [os.environ.get('ATRIUM_BIN', 'atrium'), 'admin', 'screen',
                       'navigate', args.screen, '--route', route, '--wait', '--json',
                       '--url', os.environ['ATRIUM_URL'],
                       '--data-dir', os.environ['ATRIUM_DATA_DIR']]
            row = {'sample': index, 'route': route, 'status': 'unobserved',
                   'error_code': '', 'exit_code': None}
            start = time.perf_counter_ns()
            try:
                result = subprocess.run(command, capture_output=True, text=True, timeout=30)
                row['exit_code'] = result.returncode
                try:
                    receipt = json.loads(result.stdout)
                    if not isinstance(receipt, dict):
                        raise ValueError('invalid receipt')
                    # Store only the fields needed to audit the result, not CLI
                    # stderr or arbitrary payloads that may contain private data.
                    for field in ('id', 'status', 'error_code'):
                        if isinstance(receipt.get(field), str):
                            row[field] = receipt[field]
                except ValueError:
                    row['error_code'] = 'invalid_receipt'
            except subprocess.TimeoutExpired:
                row['error_code'] = 'benchmark_timeout'
            except OSError:
                row['error_code'] = 'cli_start_failed'
            row['elapsed_ms'] = (time.perf_counter_ns()-start)/1_000_000
            raw.write(json.dumps(row)+'\n')
            raw.flush()
            if row['exit_code'] == 0 and row['status'] == 'applied':
                times.append(row['elapsed_ms'])
            else:
                failures += 1
        times.sort()
        p50 = times[math.ceil(len(times)*0.5)-1] if times else None
        p95 = times[math.ceil(len(times)*0.95)-1] if times else None
        maximum = max(times) if times else None
        def fmt(value):
            return f'{value:.3f}ms' if value is not None else 'n/a'
        verdict = 'all_applied_within_upper_bound' if not failures and p95 <= 1000 and maximum <= 3000 else 'not_established'
        line = (f'applied={len(times)} not_applied={failures} '
                f'applied_P50={fmt(p50)} applied_P95={fmt(p95)} applied_max={fmt(maximum)}\n'
                f'result={verdict}\n')
        summary.write(line)
        print(header+line, end='')
    return 0 if verdict == 'all_applied_within_upper_bound' else 1


if __name__ == '__main__':
    raise SystemExit(main())
