"""Independent original-page fidelity, structural rejection and offline drift tests."""

import ast
import hashlib
import html
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import gfm_corpus as gfm


ROOT = Path(__file__).resolve().parents[1]
RELATIVE_CORPUS = Path("internal/parser/testdata/gfm/0.29-gfm")
PIN = "b153d814fdfc8624bb6da7449162c1cd707a637f7d1c1b636eb44b9cf63fa220"


def synthetic(markdown="&amp;lt;<span class=\"space\"> </span>→\n", expected="&lt;p&gt;&amp;amp;&lt;/p&gt;\n"):
    return ('<h2 id="s"><span class="number">1.2</span>Section</h2>\n'
            '<div class="example" id="example-1">\n'
            '<div class="column"><pre><code class="language-markdown">'
            + markdown + '</code></pre></div>\n'
            '<div class="column"><pre><code class="language-html">'
            + expected + '</code></pre></div>\n</div>').encode()


class ExtractionTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source = (ROOT / RELATIVE_CORPUS / "upstream/spec.html").read_bytes()
        cls.cases = gfm.extract(cls.source)

    def test_all_original_fields_against_independent_source_slices(self):
        # Separate retained-source grammar: no production parser, scope constant,
        # decoding callbacks or generated output is used to form expectations.
        source = self.source.decode()
        pattern = re.compile(
            r'^<div class="example" id="example-([0-9]+)">\n'
            r'.*?<pre><code class="language-markdown">(.*?)</code></pre>\n</div>\n'
            r'<div class="column">\n<pre><code class="language-html">(.*?)'
            r'</code></pre>\n</div>\n</div>', re.M | re.S)
        matches = list(pattern.finditer(source))
        self.assertEqual(len(matches), 677)
        heading = re.compile(r'<h[1-6] id="[^"]*"[^>]*>(.*?)</h[1-6]>', re.S)
        for actual, match in zip(self.cases, matches):
            section = list(heading.finditer(source, 0, match.start()))[-1].group(1)
            section = re.sub(r'<span class="number">.*?</span>', '', section, flags=re.S)
            section = html.unescape(re.sub(r'<[^>]*>', '', section)).strip()
            fields = [html.unescape(re.sub(r'<span class="space">(.*?)</span>',
                                          r'\1', match.group(i), flags=re.S)).replace('→', '\t')
                      for i in (2, 3)]
            self.assertEqual(actual, {
                'example': int(match.group(1)), 'section': section,
                'start_line': source.count('\n', 0, match.start()) + 1,
                'markdown': fields[0], 'html': fields[1],
                'end_line': source.count('\n', 0, match.end()) + 1,
                'snapshot_sha256': PIN,
                'input_sha256': hashlib.sha256(fields[0].encode()).hexdigest(),
                'scope': self.expected_scope(int(match.group(1))),
            })
        self.assertEqual(json.loads((ROOT / RELATIVE_CORPUS / 'examples.json').read_bytes()), self.cases)

    @staticmethod
    def expected_scope(number):
        if 198 <= number <= 205:
            return 'tables'
        if number in (279, 280):
            return 'task-list-items'
        if 491 <= number <= 493:
            return 'strikethrough'
        if 622 <= number <= 635:
            return 'extended-autolinks'
        if number == 657:
            return 'disallowed-raw-html'
        return 'base'

    def test_exact_extension_ids_and_all_input_hashes(self):
        expected = {
            'tables': [198, 199, 200, 201, 202, 203, 204, 205],
            'task-list-items': [279, 280], 'strikethrough': [491, 492, 493],
            'extended-autolinks': [622, 623, 624, 625, 626, 627, 628, 629, 630, 631, 632, 633, 634, 635],
            'disallowed-raw-html': [657],
        }
        for scope, ids in expected.items():
            self.assertEqual([c['example'] for c in self.cases if c['scope'] == scope], ids)
        self.assertEqual(sum(c['scope'] == 'base' for c in self.cases), 649)
        self.assertEqual([c['example'] for c in self.cases], list(range(1, 678)))
        for case in self.cases:
            self.assertEqual(case['snapshot_sha256'], PIN)
            self.assertEqual(case['input_sha256'], hashlib.sha256(case['markdown'].encode()).hexdigest())
            self.assertTrue(case['markdown'].endswith('\n'))
            self.assertTrue(case['html'] == '' or case['html'].endswith('\n'))

    def test_literal_whitespace_entities_tasks_and_old_expectations(self):
        def case(n):
            return self.cases[n-1]
        self.assertEqual(case(1)['markdown'], '\tfoo\tbaz\t\tbim\n')
        self.assertEqual(case(1)['html'], '<pre><code>foo\tbaz\t\tbim\n</code></pre>\n')
        self.assertEqual(case(72)['markdown'], '\\> foo\n------\n')
        self.assertEqual(case(72)['html'], '<h2>&gt; foo</h2>\n')
        self.assertEqual(case(279)['markdown'], '- [ ] foo\n- [x] bar\n')
        self.assertEqual(case(279)['html'], '<ul>\n<li><input disabled="" type="checkbox"> foo</li>\n<li><input checked="" disabled="" type="checkbox"> bar</li>\n</ul>\n')
        self.assertEqual(case(280)['markdown'], '- [x] foo\n  - [ ] bar\n  - [x] baz\n- [ ] bim\n')
        self.assertEqual(case(280)['html'], '<ul>\n<li><input checked="" disabled="" type="checkbox"> foo\n<ul>\n<li><input disabled="" type="checkbox"> bar</li>\n<li><input checked="" disabled="" type="checkbox"> baz</li>\n</ul>\n</li>\n<li><input disabled="" type="checkbox"> bim</li>\n</ul>\n')
        self.assertEqual(case(321)['markdown'], '&nbsp; &amp; &copy; &AElig; &Dcaron;\n&frac34; &HilbertSpace; &DifferentialD;\n&ClockwiseContourIntegral; &ngE;\n')
        self.assertEqual(case(321)['html'], '<p>\u00a0 &amp; © Æ Ď\n¾ ℋ ⅆ\n∲ ≧̸</p>\n')
        self.assertEqual(case(324)['html'], '<p>&amp;nbsp &amp;x; &amp;#; &amp;#x;\n&amp;#87654321;\n&amp;#abcdef0;\n&amp;ThisIsNotDefined; &amp;hi?;</p>\n')
        self.assertEqual(case(649)['html'], '<p>foo &lt;!-- not a comment -- two hyphens --&gt;</p>\n')
        self.assertEqual(case(650)['html'], '<p>foo &lt;!--&gt; foo --&gt;</p>\n<p>foo &lt;!-- foo---&gt;</p>\n')
        self.assertEqual(case(657)['html'], '<p><strong> &lt;title> &lt;style> <em></p>\n<blockquote>\n  &lt;xmp> is disallowed.  &lt;XMP> is also disallowed.\n</blockquote>\n')
        self.assertEqual(case(677)['markdown'], 'Multiple     spaces\n')
        self.assertEqual(case(677)['html'], '<p>Multiple     spaces</p>\n')
        for n, bounds in [(1, (526, 539)), (279, (6686, 6702)), (677, (13650, 13662))]:
            self.assertEqual((case(n)['start_line'], case(n)['end_line']), bounds)
        self.assertEqual([c['example'] for c in self.cases if c['html'] == ''], [176, 188])

    def test_pure_synthetic_one_decode_presentation_and_empty_presence(self):
        actual = gfm.extract(synthetic(), expected_count=1)[0]
        self.assertEqual(actual['markdown'], '&lt; \t\n')
        self.assertEqual(actual['html'], '<p>&amp;</p>\n')
        self.assertEqual(actual['section'], 'Section')
        self.assertEqual(gfm.extract(synthetic(expected=''), expected_count=1)[0]['html'], '')

    def test_missing_duplicate_ids_and_changed_count_reach_structure(self):
        for source in [self.source.replace(b'id="example-2"', b'id="example-1"', 1),
                       self.source.replace(b'id="example-2"', b'id="example-678"', 1),
                       self.source + synthetic()]:
            with self.subTest(size=len(source)), self.assertRaisesRegex(ValueError, 'consecutive unique'):
                gfm.extract(source)
        with self.assertRaisesRegex(ValueError, 'invalid example ID'):
            gfm.extract(synthetic().replace(b'id="example-1"', b'id="missing"'), 1)
        with self.assertRaisesRegex(ValueError, '677 consecutive'):
            gfm.extract(synthetic())

    def test_missing_duplicate_fields_and_malformed_nesting(self):
        original = synthetic()
        field = b'<div class="column"><pre><code class="language-html">&lt;p&gt;&amp;amp;&lt;/p&gt;\n</code></pre></div>\n'
        self.assertIn(field, original)
        variants = [
            (original.replace(field, b''), 'missing Markdown or HTML'),
            (re.sub(rb'<div class="column"><pre><code class="language-markdown">.*?</code></pre></div>\n', b'', original, count=1, flags=re.S), 'missing Markdown or HTML'),
            (original.replace(field, field + field), 'duplicate html'),
            (original.replace(b'language-markdown', b'language-html'), 'duplicate html'),
            (original[:-6], 'truncated'),
            (original.replace(b'</pre>', b'</div>', 1), 'unbalanced'),
            (original.replace(b'<span class="space">', b'<em>'), 'unsupported nested'),
            (original.replace('</span>→'.encode(), '</em>→'.encode()), 'unbalanced'),
            (original.replace(b'<span class="space">', b'<span class="space"><span class="space">'), 'unsupported nested'),
            (original.replace(b'<div class="column">', b'<!-- hidden -->', 1), 'unsupported example comment'),
        ]
        for source, message in variants:
            with self.subTest(message=message), self.assertRaisesRegex(ValueError, message):
                gfm.extract(source, expected_count=1)

    def test_classification_guard_is_executable(self):
        with mock.patch.dict(gfm.EXTENSIONS, {198: 'base'}):
            with self.assertRaisesRegex(ValueError, 'classification differs'):
                gfm.extract(self.source)


class OfflineTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='mdchk-gfm-corpus-')
        self.addCleanup(temporary.cleanup)
        self.checkout = Path(temporary.name) / 'checkout'
        self.corpus = self.checkout / RELATIVE_CORPUS
        shutil.copytree(ROOT / RELATIVE_CORPUS, self.corpus)
        self.tool = self.checkout / 'tools/gfm_corpus.py'
        self.tool.parent.mkdir()
        shutil.copy2(ROOT / 'tools/gfm_corpus.py', self.tool)
        self.fixture = self.corpus / 'examples.json'
        self.unrelated = Path(temporary.name) / 'unrelated'
        self.unrelated.mkdir()
        self.empty_path = Path(temporary.name) / 'empty-path'
        self.empty_path.mkdir()
        self.provenance = self.corpus / 'provenance.json'

    def run_tool(self, *arguments):
        # Block actual Python socket creation and DNS, not just PATH lookup.
        runner = ('import socket,runpy,sys; '
                  'deny=lambda *a,**k: (_ for _ in ()).throw(AssertionError("network forbidden")); '
                  'socket.socket=deny; socket.create_connection=deny; socket.getaddrinfo=deny; '
                  'sys.argv=sys.argv[1:]; runpy.run_path(sys.argv[0],run_name="__main__")')
        return subprocess.run([sys.executable, '-I', '-B', '-c', runner, str(self.tool), *arguments],
                              cwd=self.unrelated, env=dict(os.environ, PATH=str(self.empty_path)),
                              capture_output=True)

    def snapshot(self):
        return {str(p.relative_to(self.checkout)): (p.read_bytes(), p.stat().st_mtime_ns)
                for p in self.checkout.rglob('*') if p.is_file()}

    def failure(self, message):
        before = self.snapshot()
        result = self.run_tool('--check')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(message, result.stderr.decode())
        self.assertEqual(before, self.snapshot())

    def test_offline_check_and_twice_absent_output_regeneration(self):
        original = self.fixture.read_bytes()
        before = self.snapshot()
        result = self.run_tool('--check')
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        self.assertEqual(before, self.snapshot())
        for _ in range(2):
            self.fixture.unlink()
            result = self.run_tool()
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertEqual(self.fixture.read_bytes(), original)
            self.assertEqual(hashlib.sha256(self.fixture.read_bytes()).hexdigest(),
                             json.loads(self.provenance.read_bytes())['generated']['sha256'])

    def test_generated_drift_removed_case_and_missing_output_read_only(self):
        original = self.fixture.read_bytes()
        for damaged in [original.replace(b'<pre>', b'<PRE>', 1),
                        json.dumps(json.loads(original)[1:]).encode()]:
            self.fixture.write_bytes(damaged)
            self.failure('examples.json differs')
        self.fixture.unlink()
        self.failure('examples.json')

    def test_source_fixed_pin_size_and_hash_before_extraction(self):
        source = self.corpus / 'upstream/spec.html'
        original = source.read_bytes()
        source.write_bytes(original + b' ')
        self.failure('byte count differs')
        source.write_bytes(b'X' + original[1:])
        # Even a co-edited provenance entry cannot change the approved pin.
        p = json.loads(self.provenance.read_bytes())
        p['inputs']['upstream/spec.html']['sha256'] = hashlib.sha256(source.read_bytes()).hexdigest()
        self.provenance.write_text(json.dumps(p))
        self.failure('SHA-256 differs')

    def test_all_inputs_and_extractor_are_verified_without_repair(self):
        for path in [self.corpus / 'upstream/cmark-gfm-COPYING', self.tool]:
            original = path.read_bytes()
            path.write_bytes(original.replace(b' ', b'\t', 1))
            self.failure('SHA-256 differs')
            path.write_bytes(original)
        (self.corpus / 'upstream/cmark-gfm-COPYING').unlink()
        self.failure('cmark-gfm-COPYING')

    def test_pinned_generated_hash_and_nondeterminism_guards(self):
        original = gfm.serialize
        with mock.patch.object(gfm, 'serialize', side_effect=[original(gfm.extract(gfm.CORPUS.joinpath('upstream/spec.html').read_bytes())), b'changed']):
            with self.assertRaisesRegex(ValueError, 'nondeterministic'):
                gfm.generate()
        with mock.patch.object(gfm, 'serialize', return_value=b'changed'):
            with self.assertRaisesRegex(ValueError, 'pinned generated digest'):
                gfm.generate()

    def test_provenance_licenses_and_fixed_hashes(self):
        p = json.loads(self.provenance.read_bytes())
        self.assertEqual(set(p['inputs']), {'upstream/spec.html', 'upstream/cmark-gfm-COPYING'})
        self.assertEqual(p['approved_snapshot_retrieved_at'], '2026-10-14')
        for name, entry in p['inputs'].items():
            content = (self.corpus / name).read_bytes()
            self.assertEqual(len(content), entry['byte_count'])
            self.assertEqual(hashlib.sha256(content).hexdigest(), entry['sha256'])
            self.assertEqual(entry['retrieved_at'], '2026-10-22')
            self.assertEqual(entry['modifications'], 'none')
            self.assertTrue(entry['origin_url'])
            self.assertTrue(entry['role'])
        page = p['inputs']['upstream/spec.html']
        self.assertIsNone(page['commit'])
        self.assertIsNone(page['repository'])
        self.assertEqual(page['sha256'], PIN)
        self.assertEqual(page['byte_count'], 508310)
        self.assertEqual(page['license'], 'CC-BY-SA-4.0')
        notice = p['inputs']['upstream/cmark-gfm-COPYING']
        self.assertEqual(notice['commit'], '587a12bb54d95ac37241377e6ddc93ea0e45439b')
        self.assertEqual(notice['license'], 'CC-BY-SA-4.0 AND BSD-2-Clause AND MIT')
        text = (self.corpus / 'upstream/cmark-gfm-COPYING').read_text()
        for phrase in ['John MacFarlane', 'Karl Dubost', 'creativecommons.org/licenses/by-sa/4.0',
                       'Redistribution and use', 'Permission is hereby granted']:
            self.assertIn(phrase, text)
        commonmark = ROOT / 'internal/parser/testdata/commonmark/0.31.2'
        cm = json.loads((commonmark / 'provenance.json').read_bytes())
        for name, entry in cm['inputs'].items():
            self.assertEqual(hashlib.sha256((commonmark / name).read_bytes()).hexdigest(), entry['sha256'])
        self.assertEqual(cm['inputs']['upstream/spec.txt']['sha256'], '257c41ad946f7a1414a499aca402a1aa8fdac3678532266611348c1cf54f4b80')
        self.assertEqual(cm['inputs']['upstream/spec.txt']['commit'], '9103e341a973013013bb1a80e13567007c5cef6f')
        self.assertEqual(p['generator']['version'], 1)
        self.assertEqual(hashlib.sha256(self.tool.read_bytes()).hexdigest(), p['generator']['sha256'])
        self.assertEqual(p['generated']['license'], 'CC-BY-SA-4.0')
        self.assertEqual(hashlib.sha256(self.fixture.read_bytes()).hexdigest(), p['generated']['sha256'])

    def test_network_and_external_program_imports_absent(self):
        tree = ast.parse(self.tool.read_text())
        imports = set()
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imports.update(alias.name for alias in node.names)
            elif isinstance(node, ast.ImportFrom):
                imports.add(node.module)
        self.assertEqual(imports, {'argparse', 'hashlib', 'html', 'html.parser', 'json', 'pathlib', 're', 'sys'})


if __name__ == '__main__':
    unittest.main()
