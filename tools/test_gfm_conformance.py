"""Independent actual-parser, projection and faulty-consumer boundary tests."""
import copy
import contextlib
import io
import sys
import json
import os
import signal
from pathlib import Path
import subprocess
import tempfile
import time
import unittest
from unittest import mock

import gfm_conformance as runner
import gfm_profile as profile


class ActualConformance(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory(prefix='mdchk-actual-test-')
        cls.adapter = Path(cls.temp.name)/'adapter'
        subprocess.run(['go', 'build', '-tags', 'mdchk_conformance', '-o', str(cls.adapter),
                        './internal/conformance/cmd/specadapter'], cwd=profile.ROOT, check=True)
        cls.originals, cls.fixtures = profile.load()
        cls.data = json.loads((profile.PROFILE/'ledger.json').read_bytes())
        cls.units = runner.inventory(cls.data, cls.originals, cls.fixtures)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def unit(self, identity):
        return next(u for u in self.units if u['id'] == identity)

    def actual(self, identity):
        u = self.unit(identity)
        a = runner.execute(self.adapter, u, 10)
        self.assertEqual(a['status'], 'rendered', a)
        return u, a

    def test_real_rendering_and_source_witnesses(self):
        identities = [f'published:{n}:integrated' for n in
                      (198, 199, 200, 201, 202, 203, 204, 205, 279, 280, 491, 492, 493, 622, 634, 635, 657)]
        identities += [f'{n}:integrated' for n in ('combined-all-five', 'modern-reference-fold',
                        'modern-entity-one-pass', 'task-uppercase-crlf', 'task-loose',
                        'strike-container-gaps', 'autolink-utf8-tab', 'table-code-pipe-crlf', 'table-nested-quote')]
        for identity in identities:
            with self.subTest(identity=identity):
                u, a = self.actual(identity)
                self.assertEqual(runner.comparison(u['target'], a, u['execution_scope'])['status'], 'pass')

    def test_historical_raw_and_general_modern_target_selection(self):
        for n in (649, 650):
            u, a = self.actual(f'published:{n}:original')
            self.assertEqual(runner.comparison(u['raw'], a, 'core')['status'], 'fail')
            self.assertEqual(runner.comparison(u['target'], a, 'core')['status'], 'pass')
            self.assertEqual(u['target_ref'], self.data['cases'][n-1]['expectations']['core'])
            broken = copy.deepcopy(a)
            broken['html'] = u['raw']['html']
            self.assertEqual(runner.comparison(u['target'], broken, 'core')['status'], 'fail')
        for n in (611, 615):
            for scope in ('original', 'integrated'):
                u, a = self.actual(f'published:{n}:{scope}')
                self.assertEqual(runner.comparison(u['target'], a, u['execution_scope'])['status'], 'pass')
            self.assertEqual(self.unit(f'published:{n}:original')['execution_scope'], 'core')
        bad = copy.deepcopy(self.data)
        bad['cases'][614]['expectations']['core'] = bad['cases'][614]['expectations']['integrated']
        with self.assertRaises(ValueError):
            runner.inventory(bad, self.originals, self.fixtures)

    def test_strict_html_mutants(self):
        u, a = self.actual('combined-all-five:integrated')
        for html in (a['html']+' ', a['html'].replace('www.a.org', 'wrong.a.org'),
                     a['html'].replace('<del>', '<em>'), a['html'].replace('\n', '\r\n'),
                     a['html'].replace('checked="" ', ''), a['html'].replace('http://', 'https://')):
            b = copy.deepcopy(a); b['html'] = html
            self.assertEqual(runner.comparison(u['target'], b, 'integrated')['status'], 'fail')

    def test_actual_fact_mutants_and_expectation_independence(self):
        u, a = self.actual('modern-reference-fold:integrated')
        link = next(f for f in a['catalog'] if f['kind'] == 'link')
        for key, value in (('destination', '/wrong'), ('visible', [0, 0, '']),
                           ('parts', [{'source': [0, 0, ''], 'value': '/target', 'transformation': 'reference'}]),
                           ('binding', {})):
            b = copy.deepcopy(a); b['catalog'][link['id']]['facts'][key] = value
            self.assertEqual(runner.comparison(u['target'], b, 'integrated')['status'], 'fail')
        b = copy.deepcopy(a); b['catalog'][link['id']]['parent'] = 0
        self.assertEqual(runner.comparison(u['target'], b, 'integrated')['status'], 'fail')
        b = copy.deepcopy(a); b['catalog'].append(copy.deepcopy(link))
        self.assertEqual(runner.comparison(u['target'], b, 'integrated')['status'], 'fail')
        expected = copy.deepcopy(u['target']); expected['nodes'][0]['children'][0]['facts']['destination'] = '/wrong'
        self.assertEqual(runner.comparison(expected, a, 'integrated')['status'], 'fail')
        self.assertEqual(a, runner.execute(self.adapter, u, 10))

    def test_core_document_only_still_rejects_extensions(self):
        u, a = self.actual('combined-all-five:core')
        b = copy.deepcopy(a)
        b['catalog'][1]['kind'] = 'strike'
        self.assertEqual(runner.comparison(u['target'], b, 'core')['status'], 'fail')
        b['catalog'][1]['kind'] = 'future-extension'
        with self.assertRaises(ValueError):
            runner.validate_actual(b, u['source'])

    def test_table_task_filter_and_source_mutants(self):
        controls = [('published:200:integrated', 'table', 'width', 99),
                    ('published:200:integrated', 'table', 'width', True),
                    ('published:279:integrated', 'task-item', 'checked', True),
                    ('published:657:integrated', 'filtered-tag', 'affected', [0, 1, '<'])]
        for identity, kind, key, value in controls:
            u, a = self.actual(identity)
            b = copy.deepcopy(a)
            fact = next(f for f in b['catalog'] if f['kind'] == kind)
            fact['facts'][key] = value
            self.assertEqual(runner.comparison(u['target'], b, 'integrated')['status'], 'fail')
        u, a = self.actual('strike-container-gaps:integrated')
        b = copy.deepcopy(a)
        fact = next(f for f in b['catalog'] if f['kind'] == 'strike')
        fact['content'] = [fact['span']]
        self.assertEqual(runner.comparison(u['target'], b, 'integrated')['status'], 'fail')
        u, a = self.actual('task-uppercase-crlf:integrated')
        b = copy.deepcopy(a)
        for position in b['positions']:
            position['column'] += 1
        self.assertEqual(runner.comparison(u['target'], b, 'integrated')['status'], 'fail')
        b = copy.deepcopy(a); b['positions'].pop()
        with self.assertRaises(ValueError):runner.validate_actual(b, u['source'])
        b = copy.deepcopy(a); b['html'] = '\ud800'
        with self.assertRaises(ValueError):runner.validate_actual(b, u['source'])
        b = copy.deepcopy(a); b['schema_version'] = True
        with self.assertRaises(ValueError):runner.validate_actual(b, u['source'])
        b = copy.deepcopy(a); b['reason'] = 'cancelled'
        with self.assertRaises(ValueError):runner.validate_actual(b, u['source'])
        b = copy.deepcopy(a); b['status'] = 'interrupted'
        with self.assertRaises(ValueError):runner.validate_actual(b, u['source'])

    def test_missing_and_subset_cannot_complete(self):
        u, a = self.actual('published:198:integrated')
        r = runner.report(self.units, [u], {u['id']: a})
        self.assertEqual(r['counts']['approved_target']['pass'], 1)
        self.assertFalse(r['approved_profile_conformance_complete'])
        with self.assertRaises(ValueError):
            runner.report(self.units, [u], {'foreign:core': a})
        r = runner.report(self.units, [u], {})
        self.assertEqual(r['counts']['approved_target']['pending'], 1)
        bad = copy.deepcopy(self.data); bad['cases'].pop()
        with self.assertRaises(ValueError):runner.inventory(bad, self.originals, self.fixtures)
        bad = copy.deepcopy(self.data); bad['cases'][1] = bad['cases'][0]
        with self.assertRaises(ValueError):runner.inventory(bad, self.originals, self.fixtures)

    @unittest.skipUnless(os.name == 'posix', 'executable fault injection uses POSIX script')
    def test_faulty_consumers_and_owned_child_cleanup(self):
        unit = self.unit('published:198:integrated')
        with tempfile.TemporaryDirectory() as temp:
            script = Path(temp)/'adapter'
            for code in ('print("not JSON")', 'print("{}")', 'print("NaN")', 'print("1e9999")', 'print("9"*10000)', 'print("["*2000+"0"+"]"*2000)', 'raise SystemExit(9)',
                         'import time; time.sleep(10)',
                         'import sys; sys.stdout.write("x"*(17<<20))',
                         'import sys; sys.stderr.write("x"*(17<<20))'):
                script.write_text('#!/usr/bin/env python3\n'+code+'\n');script.chmod(0o700)
                result = runner.execute(script, unit, .2)
                self.assertEqual(result['status'], 'error', code)
            for delayed in (False, True):
                with self.subTest(delayed_start=delayed):
                    pidfile = Path(temp)/f'pid-{delayed}'
                    gate = Path(temp)/'gate'
                    script.write_text('#!/usr/bin/env python3\nimport os,subprocess,time\n'
                                      + (f'while not os.path.exists({str(gate)!r}): time.sleep(.01)\n' if delayed else '')
                                      + 'p=subprocess.Popen(["sleep","30"])\n'
                                      + f'with open({str(pidfile)+".tmp"!r},"w") as f: f.write(str(p.pid))\n'
                                      + f'os.replace({str(pidfile)+".tmp"!r},{str(pidfile)!r})\ntime.sleep(30)\n')

                    def release_start(process):
                        if delayed:
                            # Establish setup lasting longer than the real .3s
                            # execution timeout without guessing startup speed.
                            with self.assertRaises(subprocess.TimeoutExpired):
                                process.wait(timeout=.6)
                            self.assertFalse(pidfile.exists())
                            gate.touch()

                    result = self.execute_ready_child(script, unit, pidfile, release_start)
                    self.assertEqual(result['status'], 'error')
                    self.assertEqual(result['reason'], 'adapter-execution')
                    self.assertIn('timed out after 0.3 seconds', result['message'])
                    self.assert_child_exited(int(pidfile.read_text()))

    @staticmethod
    def wait_for_child_marker(process, marker, timeout=30):
        deadline = time.monotonic()+timeout
        while not marker.exists():
            if process.poll() is not None:
                raise AssertionError('adapter exited before child readiness')
            if time.monotonic() >= deadline:
                raise AssertionError('adapter child readiness timed out')
            time.sleep(.01)

    def execute_ready_child(self, script, unit, marker, before_ready=lambda process: None, readiness_timeout=30):
        # Only setup is synchronized. The runner receives a real isolated Popen
        # and still owns its unchanged .3s execution timeout and group cleanup.
        original_popen = subprocess.Popen

        def ready_popen(*args, **kwargs):
            process = original_popen(*args, **kwargs)
            try:
                before_ready(process)
                self.wait_for_child_marker(process, marker, readiness_timeout)
                self.assertGreater(int(marker.read_text()), 0)
                return process
            except BaseException:
                # Until Popen returns, execute has no process handle to clean.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                finally:
                    try:
                        process.wait(timeout=5)
                    finally:
                        for stream in (process.stdin, process.stdout, process.stderr):
                            if stream is not None:
                                stream.close()
                raise

        with mock.patch.object(runner.subprocess, 'Popen', side_effect=ready_popen):
            return runner.execute(script, unit, .3)

    def assert_child_exited(self, pid):
        # A zombie has exited; Darwin reaps these immediately in normal runs.
        state = subprocess.run(['ps', '-o', 'stat=', '-p', str(pid)], capture_output=True, text=True).stdout.strip()
        self.assertTrue(not state or state.startswith('Z'), state)

    @unittest.skipUnless(os.name == 'posix', 'executable fault injection uses POSIX script')
    def test_child_readiness_failure_cleans_owned_group(self):
        unit = self.unit('published:198:integrated')
        with tempfile.TemporaryDirectory() as temp:
            script, owned, missing = (Path(temp)/name for name in ('adapter', 'owned', 'missing'))
            script.write_text('#!/usr/bin/env python3\nimport os,subprocess,time\n'
                              'p=subprocess.Popen(["sleep","30"])\n'
                              f'with open({str(owned)+".tmp"!r},"w") as f: f.write(str(p.pid))\n'
                              f'os.replace({str(owned)+".tmp"!r},{str(owned)!r})\ntime.sleep(30)\n')
            script.chmod(0o700)
            started = []

            def record_owned(process):
                started.append(process)
                self.wait_for_child_marker(process, owned)

            with self.assertRaisesRegex(AssertionError, 'child readiness timed out'):
                self.execute_ready_child(script, unit, missing, record_owned, readiness_timeout=.1)
            self.assertEqual(len(started), 1)
            self.assertIsNotNone(started[0].poll())
            self.assertTrue(all(stream.closed for stream in
                                (started[0].stdin, started[0].stdout, started[0].stderr)))
            self.assert_child_exited(int(owned.read_text()))

    def test_cumulative_retention_is_explicit_error(self):
        u, a = self.actual('published:198:integrated')
        with mock.patch.object(runner, 'MAX_RETAINED_ACTUAL', 1):
            actuals = runner.execute_selected(self.adapter, [u], 10)
        self.assertEqual(actuals[u['id']]['reason'], 'actual-retention-limit')
        report = runner.report(self.units, [u], actuals)
        self.assertEqual(report['counts']['approved_target']['error'], 1)
        self.assertFalse(report['approved_profile_conformance_complete'])

    def test_explicit_empty_filters_fail_before_adapter_work(self):
        intersections = {'examples': ['--fixture', 'combined-all-five'],
                         'section': ['--examples', '198', '--scope', 'original'],
                         'fixture': ['--examples', '198', '--scope', 'original']}
        for flag, other in intersections.items():
            for value in ('', ' ', '\t\n'):
                for extra in ([], other):
                    for prebuilt in (False, True):
                        for delegated in (False, True):
                            with self.subTest(flag=flag, value=value, extra=extra,
                                              prebuilt=prebuilt, delegated=delegated):
                                args = ['--'+flag, value]+extra
                                if prebuilt:
                                    args += ['--adapter', str(self.adapter)]
                                stderr = io.StringIO()
                                with mock.patch.object(runner.subprocess, 'run', side_effect=AssertionError('adapter build invoked')) as build, \
                                     mock.patch.object(runner, 'execute_selected', side_effect=AssertionError('adapter invoked')) as execute, \
                                     contextlib.redirect_stderr(stderr):
                                    if delegated:
                                        with mock.patch.object(sys, 'argv', ['gfm_profile.py', '--run']+args):
                                            code = profile.main()
                                    else:
                                        code = runner.main(args)
                                self.assertNotEqual(code, 0)
                                self.assertIn('--'+flag, stderr.getvalue())
                                build.assert_not_called()
                                execute.assert_not_called()

    def test_offline_selection_unknown_and_denied_socket(self):
        with mock.patch('socket.socket', side_effect=AssertionError('network forbidden')):
            self.assertEqual(len(runner.inventory(self.data, self.originals, self.fixtures)), 2117)
            u, a = self.actual('published:622:original')
            self.assertEqual(runner.comparison(u['target'], a, 'bare-only')['status'], 'pass')
        with tempfile.TemporaryDirectory() as cwd:
            command = ['python3', '-B', str(profile.ROOT/'tools/gfm_conformance.py'),
                       '--adapter', str(self.adapter), '--examples', '649,650', '--json']
            first = subprocess.run(command, cwd=cwd, capture_output=True, check=True).stdout
            self.assertEqual(first, subprocess.run(command, cwd=cwd, capture_output=True, check=True).stdout)
            r = json.loads(first)
            self.assertEqual(r['counts']['raw_declared']['fail'], 2)
            self.assertEqual(r['counts']['approved_target']['pass'], 6)
            for flags in (['--examples','678'], ['--scope','foreign'], ['--section','foreign'],
                          ['--examples','198','--fixture','task-loose']):
                self.assertNotEqual(subprocess.run(command[:5]+flags, cwd=cwd, capture_output=True).returncode, 0)


if __name__ == '__main__':
    unittest.main()
