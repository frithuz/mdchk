"""Authored-data validity, semantic mutation and isolated optional-oracle controls.

No test executes Pandoc or an mdchk Pandoc parser. Synthetic subprocess responses
exercise only the optional tool boundary and never establish oracle observations.
"""

import copy
import base64
import contextlib
import io
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import pandoc_profile as p


class ContractTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.profile, cls.cases, cls.coverage, cls.provenance = p.load()

    def case(self, name):
        case = copy.deepcopy(self.cases[name])
        return case, (p.CORPUS/case['input']).read_bytes()

    def reject(self, name, mutate, message):
        case, source = self.case(name)
        mutate(case)
        with self.assertRaisesRegex(ValueError, message):
            p.validate_case(case, source)

    def test_full_inventory_and_honest_status(self):
        self.assertEqual(len(self.coverage['extensions']), 74)
        self.assertEqual(sum(x['enabled'] for x in self.coverage['extensions']), 37)
        self.assertEqual(len(self.cases), 543)
        self.assertTrue(all(row['dimensions']['escape']['cases'] for row in self.coverage['extensions'] if row['enabled']))
        self.assertEqual(set(self.profile['upstream_defaults']),
                         {x['extension'] for x in self.coverage['extensions']})
        derived = p.ledger(self.profile, self.cases, self.coverage)
        self.assertEqual(derived['actual_parser_passes'], 0)
        self.assertFalse(derived['conformance_complete'])
        self.assertTrue(all(c['actual_parser'] == 'NOT-RUN/unimplemented' for c in derived['cases']))
        self.assertEqual(p.serialize(derived), (p.CORPUS/'ledger.json').read_bytes())

    def test_literal_loss_sensitive_targets(self):
        nbsp = self.cases['notes-unicode-space']['facts']['notes']
        self.assertEqual(nbsp['definitions'][0]['label'][2], 'a\u00a0b')
        self.assertEqual(nbsp['references'][0]['binding'], 'nbsp')
        notes = self.cases['notes-all-records']['facts']['notes']
        self.assertEqual([d['label'][2] for d in notes['definitions']],
                         ['a', 'unused', 'empty', 'a', 'A', 'é', 'é', 'a\\b', 'quote', 'list', 'outer', 'inner'])
        self.assertEqual([r['binding'] for r in notes['references'][:6]], ['d3', 'd4', 'd5', 'd6', 'd7', None])
        self.assertEqual(notes['references'][6]['context'], 'named-note')
        self.assertIsNone(notes['references'][6]['binding'])
        self.assertEqual([n['empty'] for n in notes['inline']], [False, True, True])
        for name, width in [('short', 1), ('excess', 3)]:
            rows = self.cases['table-loss-'+name]['facts']['tables'][0]['rows']
            self.assertEqual([len(r['cells']) for r in rows], [width, 2, 1, 3])
            self.assertEqual(rows[2]['semantic'], [0, None])
            self.assertEqual(rows[3]['semantic'], [0, 1])
            self.assertIn('`a|b` $c|d$', rows[3]['cells'][0][2])
            self.assertEqual(len(rows[3]['protected']), 5)
        tasks = self.cases['tasks-original-crlf']['facts']['tasks']
        self.assertEqual([t['marker'][2] for t in tasks], ['[X]', '[x]', '[ ]'])
        self.assertEqual([t['checked'] for t in tasks], [True, True, False])
        self.assertIn('thematic-break', self.cases['yaml-metadata-block-excluded']['target']['structure'])
        self.assertIn('setext', self.cases['yaml-metadata-block-excluded']['target']['meaning'])

    def test_literal_list_markers_and_attribute_protection(self):
        assertions = self.cases['list-markers-original']['assertions'][1:]
        self.assertEqual([x['span'][2] for x in assertions], ['c.', 'iv.', '7.', '(@bird)', '(@bird)'])
        self.assertEqual([x['role'] for x in assertions],
                         ['alphabetic-marker', 'roman-marker', 'decimal-marker', 'example-marker', 'example-reference'])
        for family in ('heading', 'block-code', 'inline-code', 'link', 'image', 'span', 'div', 'table'):
            facts = self.cases['raw-ids-'+family]['facts']
            self.assertEqual(facts['protected'][0]['owner'], facts['attributes'][0]['span'])
            self.assertEqual(facts['protected'][0]['literal'], '#first #last')
        self.assertEqual(self.cases['toml-looking-ordinary']['target']['structure'], ['paragraph'])

    def test_reviewed_raw_fence_and_escape_fallback(self):
        case, source = self.case('protected-raw-format')
        self.assertEqual(source, b'```{=latex}\n[^x] \\alpha\n```\nordinary '+ 'é\n'.encode())
        protection = case['facts']['protected'][0]
        self.assertEqual(protection['owner'], [0, 28, '```{=latex}\n[^x] \\alpha\n```\n'])
        self.assertEqual(protection['closing'], [24, 28, '```\n'])
        self.assertEqual(case['assertions'][1]['span'], [28, 40, 'ordinary é\n'])
        fallback = self.cases['all-symbols-escapable-fallback']
        self.assertEqual(fallback['target']['structure'], ['paragraph', 'raw'])
        self.assertEqual(fallback['facts']['protected'][0]['payload'], [0, 2, '\\a'])
        self.assertEqual(self.cases['all-symbols-escapable-positive']['facts']['protected'], [])
        self.assertIn('Escaped symbols contribute', self.cases['all-symbols-escapable-positive']['target']['meaning'])

    def test_reviewed_exclusion_escape_dispositions(self):
        expected = {'grid_tables': ('\\+---+---+\n', ['paragraph', 'text']),
                    'implicit_figures': ('\\![bird](bird.png "caption")\n', ['paragraph', 'text', 'link']),
                    'mark': ('\\==bird==\n', ['paragraph', 'text'])}
        for extension, (source, structure) in expected.items():
            row = next(r for r in self.coverage['extensions'] if r['extension'] == extension)
            identity = extension.replace('_', '-')+'-escape'
            self.assertEqual(row['dimensions']['escape'], dict(cases=[identity], not_applicable=None))
            case = self.cases[identity]
            self.assertEqual((p.CORPUS/case['input']).read_bytes(), source.encode())
            self.assertEqual(case['target']['structure'], structure)
            self.assertEqual(case['assertions'][0]['span'], [0, len(source.encode()), source])
        self.assertTrue(all(r['dimensions']['escape']['cases'] for r in self.coverage['extensions']))

    def test_no_subprocess_or_network_in_integrity(self):
        with patch.object(subprocess, 'run', side_effect=AssertionError('subprocess forbidden')), \
                patch.object(socket, 'socket', side_effect=AssertionError('network forbidden')):
            profile, cases, coverage, _ = p.load()
            p.ledger(profile, cases, coverage)

    def test_coverage_mutations_before_selection(self):
        mutations = [
            (lambda d: d['extensions'].pop(), 'all 74'),
            (lambda d: d['extensions'].__setitem__(1, copy.deepcopy(d['extensions'][0])), 'duplicate extension'),
            (lambda d: d['extensions'][0].__setitem__('extension', 'foreign'), 'unknown/duplicate'),
            (lambda d: d['extensions'][0].__setitem__('enabled', True), 'wrong state'),
            (lambda d: d['extensions'][0].__setitem__('cases', []), 'empty/duplicate'),
            (lambda d: d['extensions'][0]['cases'].append(d['extensions'][0]['cases'][0]), 'empty/duplicate'),
            (lambda d: d['extensions'][0]['cases'].append('missing'), 'dangling'),
            (lambda d: d['extensions'][0]['dimensions']['positive'].__setitem__('not_applicable', ''), 'dimension: dangling'),
        ]
        for mutate, message in mutations:
            data = copy.deepcopy(self.coverage)
            mutate(data)
            with self.subTest(message=message), self.assertRaisesRegex(ValueError, message):
                p.validate_ownership(data, self.cases, self.profile)
        cases = dict(self.cases, foreign=self.cases[next(iter(self.cases))])
        with self.assertRaisesRegex(ValueError, 'unowned'):
            p.validate_ownership(self.coverage, cases, self.profile)

    def test_lost_unused_duplicate_or_empty_note_rejected_beyond_hash(self):
        for index in (0, 1, 2, 3):
            with self.subTest(index=index):
                self.reject('notes-all-records', lambda c: c['facts']['notes']['definitions'].pop(index), 'lost|binding')

    def test_note_binding_and_eligibility(self):
        self.reject('notes-all-records', lambda c: c['facts']['notes']['references'][0].__setitem__('binding', 'd0'), 'last exact-label binding')
        self.reject('notes-all-records', lambda c: c['facts']['notes']['references'][6].__setitem__('eligible', True), 'eligibility')
        self.reject('notes-all-records', lambda c: c['facts']['notes']['references'][6].__setitem__('binding', 'd4'), 'binding')
        self.reject('notes-all-records', lambda c: c['facts']['notes']['inline'][1].__setitem__('empty', False), 'empty content')
        self.reject('notes-all-records', lambda c: c['facts']['notes']['definitions'][8].__setitem__('owner', c['source']['envelope']), 'physical container')

    def test_lost_raw_table_cells_and_padding(self):
        self.reject('table-loss-excess', lambda c: c['facts']['tables'][0]['rows'][0]['cells'].pop(), 'lost original cell')
        self.reject('table-loss-short', lambda c: c['facts']['tables'][0]['rows'][2].__setitem__('semantic', [0, 0]), 'spanless null')
        self.reject('table-loss-short', lambda c: c['facts']['tables'][0].__setitem__('width', 3), 'delimiter-derived width|spanless null')
        self.reject('table-loss-short', lambda c: c['facts']['tables'][0]['rows'][3].__setitem__('protected', []), 'unowned separator')
        self.reject('table-loss-short', lambda c: c['facts']['tables'][0]['caption'].__setitem__(1, 0), 'bounds')

    def test_task_state_and_owner(self):
        self.reject('tasks-original-crlf', lambda c: c['facts']['tasks'][0].__setitem__('checked', False), 'checked state')
        self.reject('tasks-original-crlf', lambda c: (c['facts']['tasks'][0].__setitem__('owner', [0, 2, '> ']), c['fact_inventory']['tasks'].__setitem__(0, [0, 2, '> '])), 'source owner')
        self.reject('tasks-original-crlf', lambda c: c['facts']['tasks'][0]['marker'].__setitem__(2, '[x]'), 'original bytes')

    def test_raw_id_order_and_last_effective_id(self):
        self.reject('raw-ids-heading', lambda c: c['facts']['attributes'][0].__setitem__('effective', 'first'), 'last effective ID')
        self.reject('raw-ids-heading', lambda c: c['facts']['attributes'][0]['ids'].reverse(), 'overlap or order')
        self.reject('raw-ids-heading', lambda c: c['facts']['attributes'][0]['ids'].pop(0), 'lost raw occurrence')

    def test_html_origin_opaque_and_children(self):
        self.reject('html-origin-div', lambda c: c['facts']['html'][0].__setitem__('origin', None), 'physical origin')
        self.reject('html-origin-span', lambda c: c['facts']['html'][0].__setitem__('children', []), 'child eligibility')
        self.reject('html-origin-script', lambda c: c['facts']['html'][0].__setitem__('opaque', False), 'opaque payload')
        self.reject('html-origin-script', lambda c: c['facts']['html'][0].__setitem__('children', [c['facts']['html'][0]['payload']]), 'child eligibility')

    def test_protection_lost_payload_or_swallowed_prose(self):
        self.reject('protected-math', lambda c: c['facts']['protected'][0].__setitem__('literal', ''), 'literal payload')
        self.reject('protected-math', lambda c: (c['facts']['protected'][0].__setitem__('owner', c['source']['envelope']), c['fact_inventory']['protected'].__setitem__(0, c['source']['envelope'])), 'payload/marker boundaries')
        self.reject('protected-citation', lambda c: c['facts']['protected'][0].pop('payload'), 'missing or unknown')

    def test_invalid_bounds_scalar_positions_and_unknown_fields(self):
        self.reject('tasks-original-crlf', lambda c: c['source']['envelope'].__setitem__(1, 100000), 'bounds')
        self.reject('tasks-original-crlf', lambda c: c['source']['positions'].append(dict(offset=9,line=1,column=9)), 'scalar interior')
        self.reject('tasks-original-crlf', lambda c: c['source']['positions'].append(dict(offset=11,line=2,column=1)), 'CRLF interior')
        self.reject('tasks-original-crlf', lambda c: c['source'].__setitem__('gaps', []), 'partition|missing original')
        self.reject('raw-ids-heading', lambda c: c['target'].__setitem__('fake', True), 'missing or unknown')
        self.reject('raw-ids-heading', lambda c: c.__setitem__('assertions', []), 'source assertions required')
        self.reject('raw-ids-heading', lambda c: c.__setitem__('input_sha256', '0'*64), 'input hash')

    def test_entire_sensitive_collections_cannot_disappear(self):
        for identity, key in [('notes-all-records', 'notes'), ('raw-ids-heading', 'attributes'),
                              ('table-loss-short', 'tables'), ('tasks-original-crlf', 'tasks'),
                              ('html-origin-div', 'html'), ('protected-math', 'protected')]:
            with self.subTest(key=key):
                self.reject(identity, lambda c: c['facts'].__setitem__(key, None if key == 'notes' else []),
                            'fact inventory: missing')

    def test_note_labels_reject_exact_forbidden_characters(self):
        # Constructed contract controls, not recognition by a Markdown parser.
        for forbidden in (' ', '\t', '\r', '\n', '^', '[', ']'):
            with self.subTest(character=repr(forbidden)):
                label = 'a'+forbidden+'b'
                source = ('[^'+label+']: note\n').encode()
                owner = [0, len(source), source.decode()]
                label_end = 2+len(label.encode())
                content_start = source.index(b'note')
                definition = dict(id='invalid', span=owner,
                    label=[2, label_end, label],
                    content=[[content_start, len(source)-1, 'note']],
                    owner=owner, container='document', empty=False)
                notes = dict(definitions=[definition], references=[], inline=[],
                    inventory=dict(definitions=[owner], references=[], inline=[]))
                with self.assertRaisesRegex(ValueError, 'invalid label'):
                    p.validate_notes(source, notes)

    def test_strict_json(self):
        for raw in (b'{"a":1,"a":2}', b'{"a":NaN}'):
            with self.assertRaises(ValueError):
                p.decode(raw)


class OfflineTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='mdchk-pandoc-offline-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)/'checkout'
        self.corpus = self.root/p.CORPUS.relative_to(p.ROOT)
        shutil.copytree(p.CORPUS, self.corpus)
        for name in (p.PROFILE, 'tools/pandoc_profile.py', 'tools/test_pandoc_profile.py'):
            dest = self.root/name
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(p.ROOT/name, dest)
        self.tool = self.root/'tools/pandoc_profile.py'
        self.unrelated = Path(self.temp.name)/'unrelated'
        self.unrelated.mkdir()
        self.empty = Path(self.temp.name)/'empty-path'
        self.empty.mkdir()

    def run_tool(self, *args):
        return subprocess.run([sys.executable, '-B', str(self.tool), *args], cwd=self.unrelated,
                              env=dict(os.environ, PATH=str(self.empty)), capture_output=True, timeout=30)

    def snapshot(self):
        return {str(path.relative_to(self.root)): (path.read_bytes(), path.stat().st_mtime_ns)
                for path in self.root.rglob('*') if path.is_file()}

    def failure(self, message):
        before = self.snapshot()
        result = self.run_tool('--check')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(message, result.stderr.decode())
        self.assertEqual(self.snapshot(), before)

    def test_read_only_success_and_no_working_or_pandoc(self):
        before = self.snapshot()
        result = self.run_tool('--check', '--json')
        self.assertEqual(result.returncode, 0, result.stderr.decode())
        self.assertEqual(self.snapshot(), before)
        self.assertFalse((self.root/'Working').exists())
        self.assertFalse(json.loads(result.stdout)['conformance_complete'])

    def test_twice_generated_exact_bytes_from_retained_only(self):
        path = self.corpus/'ledger.json'
        original = path.read_bytes()
        for _ in range(2):
            path.unlink()
            result = self.run_tool()
            self.assertEqual(result.returncode, 0, result.stderr.decode())
            self.assertEqual(path.read_bytes(), original)

    def test_expected_byte_change(self):
        path = self.corpus/'expectations.json'
        path.write_bytes(path.read_bytes().replace(b'Original mdchk', b'original mdchk', 1))
        self.failure('SHA-256 differs')

    def test_removed_case(self):
        path = self.corpus/'expectations.json'
        data = json.loads(path.read_bytes())
        data['cases'].pop()
        path.write_bytes(p.serialize(data))
        self.failure('inputs: missing or unexpected')

    def test_all_retained_references_same_length_corruption(self):
        for name in sorted(p.UPSTREAM):
            path = self.corpus/'upstream'/name
            raw = path.read_bytes()
            path.write_bytes(bytes([raw[0] ^ 1])+raw[1:])
            with self.subTest(name=name):
                self.failure('SHA-256 differs')
            path.write_bytes(raw)

    def test_missing_notice(self):
        (self.corpus/'upstream/COPYRIGHT').unlink()
        self.failure('COPYRIGHT')

    def test_malformed_manifest(self):
        path = self.corpus/'provenance.json'
        data = json.loads(path.read_bytes())
        data['references'].pop('COPYING.md')
        path.write_bytes(p.serialize(data))
        self.failure('complete upstream references/notices')

    def test_profile_and_generated_drift(self):
        path = self.root/p.PROFILE
        original = path.read_bytes()
        path.write_bytes(original+b' ')
        self.failure('approved profile byte pin')
        path.write_bytes(original)
        path = self.corpus/'ledger.json'
        path.write_bytes(path.read_bytes()+b' ')
        self.failure('byte count differs')

    def test_completion_never_accepts_expectation_echo_or_missing_parser(self):
        before = self.snapshot()
        result = self.run_tool('--check', '--require-complete')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('actual Pandoc parser NOT-RUN', result.stderr.decode())
        self.assertEqual(self.snapshot(), before)
        result = self.run_tool('--actual', str(self.corpus/'expectations.json'))
        self.assertNotEqual(result.returncode, 0)  # No fake-output import surface.


class OracleBoundaryTests(unittest.TestCase):
    """Synthetic boundary checks only; these are not real oracle evidence."""

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='mdchk-pandoc-synthetic-')
        self.addCleanup(self.temp.cleanup)
        self.exe = Path(self.temp.name)/'pandoc'
        self.exe.write_bytes(b'synthetic; never executed')
        self.archive = Path(self.temp.name)/'synthetic.zip'
        with zipfile.ZipFile(self.archive, 'w') as zipped:
            zipped.writestr('pandoc-3.12.1/bin/pandoc', self.exe.read_bytes())
        self.profile = p.validate_profile((p.ROOT/p.PROFILE).read_bytes())

    def test_real_pin_rejects_synthetic_zip_before_invocation(self):
        with patch.object(subprocess, 'run', side_effect=AssertionError('must not run')):
            with self.assertRaisesRegex(ValueError, 'ZIP digest'):
                p.run_oracle(self.exe, self.archive, b'x', self.profile)

    def synthetic(self, stdout, exit_code=0, version=b'pandoc 3.12.1\n'):
        responses = [subprocess.CompletedProcess([], 0, version, b''),
                     subprocess.CompletedProcess([], exit_code, stdout, b'synthetic diagnostic')]
        with patch.object(p, 'ZIP_PIN', p.digest(self.archive.read_bytes())), \
                patch.object(subprocess, 'run', side_effect=responses) as run:
            result = p.run_oracle(self.exe, self.archive, b'original', self.profile)
        return result, run

    def test_exact_command_and_distinct_raw_observation(self):
        raw = b'{"pandoc-api-version":[1,23],"meta":{},"blocks":[]}\n'
        result, run = self.synthetic(raw)
        self.assertTrue(result['valid_observation'])
        self.assertEqual(result['input_sha256'], p.digest(b'original'))
        command = run.call_args_list[1].args[0]
        self.assertEqual(command[1], '--from='+self.profile['reader_argument'])
        self.assertEqual(command[2:4], ['--to=json', '--sandbox'])
        self.assertEqual(command[-2:], ['--columns=80', '--tab-stop=4'])
        self.assertTrue(command[4].startswith('--data-dir='))
        self.assertEqual(run.call_args_list[1].kwargs['input'], b'original')
        self.assertNotIn('--citeproc', command)
        self.assertIn('not mdchk conformance', result['kind'])

    def test_nonzero_partial_malformed_and_nonutf8_output(self):
        for stdout, status in [(b'{"blocks":[]}', 0), (b'{', 0), (b'\xff', 0),
                               (b'{"pandoc-api-version":[1],"meta":{},"blocks":[]}', 2)]:
            with self.subTest(stdout=stdout, status=status):
                result, _ = self.synthetic(stdout, status)
                self.assertFalse(result['valid_observation'])
                self.assertTrue(result['stdout_base64'])

    def nested_document(self):
        # Independent JSON literals cover the public constructor families;
        # no real executable generated these synthetic boundary test values.
        attr = ['', [], []]
        word = {'t': 'Str', 'c': 'bird'}
        para = {'t': 'Para', 'c': [word]}
        enum = lambda name: {'t': name}
        inline = [word, enum('Space'), enum('SoftBreak'), enum('LineBreak')]
        inline += [{'t': name, 'c': [word]} for name in
                   ('Emph', 'Underline', 'Strong', 'Strikeout', 'Superscript', 'Subscript', 'SmallCaps')]
        inline += [{'t': 'Quoted', 'c': [enum('DoubleQuote'), [word]]},
                   {'t': 'Code', 'c': [attr, 'raw']},
                   {'t': 'Math', 'c': [enum('InlineMath'), 'x']},
                   {'t': 'Math', 'c': [enum('DisplayMath'), 'x']},
                   {'t': 'RawInline', 'c': ['tex', '\\a']},
                   {'t': 'Link', 'c': [attr, [word], ['/bird', 'title']]},
                   {'t': 'Image', 'c': [attr, [word], ['bird.png', '']]},
                   {'t': 'Note', 'c': [para]}, {'t': 'Span', 'c': [attr, [word]]},
                   {'t': 'Cite', 'c': [[{'citationId': 'bird', 'citationPrefix': [word],
                       'citationSuffix': [], 'citationMode': enum('NormalCitation'),
                       'citationNoteNum': 1, 'citationHash': 0}], [word]]}]
        row = [attr, [[attr, enum('AlignDefault'), 1, 1, [para]]]]
        blocks = [{'t': 'Para', 'c': inline}, {'t': 'Plain', 'c': [word]},
                  {'t': 'LineBlock', 'c': [[word], []]}, {'t': 'CodeBlock', 'c': [attr, 'raw']},
                  {'t': 'RawBlock', 'c': ['html', '<b>']}, {'t': 'BlockQuote', 'c': [para]},
                  {'t': 'BulletList', 'c': [[para]]}, {'t': 'DefinitionList', 'c': [[[word], [[para]]]]},
                  {'t': 'Header', 'c': [2, attr, [word]]}, enum('HorizontalRule'),
                  {'t': 'Div', 'c': [attr, [para]]}, {'t': 'Figure', 'c': [attr, [None, [para]], [para]]},
                  {'t': 'Table', 'c': [attr, [[word], [para]],
                      [[enum('AlignLeft'), enum('ColWidthDefault')],
                       [enum('AlignRight'), {'t': 'ColWidth', 'c': 0.5}]],
                      [attr, [row]], [[attr, 0, [], [row]]], [attr, []]]}]
        for style in ('DefaultStyle', 'Example', 'Decimal', 'LowerRoman', 'UpperRoman', 'LowerAlpha', 'UpperAlpha'):
            for delim in ('DefaultDelim', 'Period', 'OneParen', 'TwoParens'):
                blocks.append({'t': 'OrderedList', 'c': [[3, enum(style), enum(delim)], [[para]]]})
        return {'pandoc-api-version': [1, 23, 1, 2], 'blocks': blocks,
                'meta': {'map': {'t': 'MetaMap', 'c': {'title': {'t': 'MetaString', 'c': 'Bird'}}},
                         'list': {'t': 'MetaList', 'c': [{'t': 'MetaBool', 'c': True}]},
                         'inlines': {'t': 'MetaInlines', 'c': [word]},
                         'blocks': {'t': 'MetaBlocks', 'c': [para]}}}

    def test_nested_valid_document_all_selected_families(self):
        result, _ = self.synthetic(p.serialize(self.nested_document()))
        self.assertTrue(result['valid_observation'])
        self.assertEqual(result['outcome'], 'success')

    def test_parseable_malformed_ast_retains_raw_invalid(self):
        documents = [
            {'pandoc-api-version': ['bogus'], 'meta': {}, 'blocks': [None]},
            {'pandoc-api-version': [], 'meta': {'a': None}, 'blocks': [{'t': 'NotAPandocNode'}]},
        ]
        for version in ([1], [1, 23, True], [1, 23, -1], [1, 24], [1.0, 23]):
            documents.append({'pandoc-api-version': version, 'meta': {}, 'blocks': []})
        for metadata, blocks in [({'a': None}, []), ({'a': {'t': 'MetaBool', 'c': 1}}, []),
                                 ({}, [None]), ({}, [{'t': 'Unknown'}]),
                                 ({}, [{'t': 'Para', 'c': [None]}]),
                                 ({}, [{'t': 'CodeBlock', 'c': [['', [], [[1, 'x']]], 'raw']}]),
                                 ({}, [{'t': 'Table', 'c': []}]),
                                 ({}, [{'t': 'Para', 'c': [{'t': 'Str', 'c': True}]}])]:
            documents.append({'pandoc-api-version': [1, 23], 'meta': metadata, 'blocks': blocks})
        for document in documents:
            raw = p.serialize(document)
            with self.subTest(document=document):
                result, _ = self.synthetic(raw)
                self.assertFalse(result['valid_observation'])
                self.assertEqual(result['outcome'], 'malformed')
                self.assertEqual(base64.b64decode(result['stdout_base64']), raw)

    def test_malformed_nested_metadata_citation_and_table_values(self):
        documents = []
        document = self.nested_document()
        document['meta']['list']['c'] = [None]
        documents.append(document)
        document = self.nested_document()
        document['blocks'][0]['c'][-1]['c'][0][0]['citationHash'] = True
        documents.append(document)
        document = self.nested_document()
        table = next(n for n in document['blocks'] if n['t'] == 'Table')
        table['c'][3][1][0][1][0][4] = [None]
        documents.append(document)
        for document in documents:
            with self.subTest(document=document):
                result, _ = self.synthetic(p.serialize(document))
                self.assertFalse(result['valid_observation'])
        raw = b'{"pandoc-api-version":[1,23],"meta":{},"blocks":[{"t":"Para","c":[{"t":"Str","c":"\\ud800"}]}]}'
        result, _ = self.synthetic(raw)
        self.assertFalse(result['valid_observation'])
        self.assertEqual(base64.b64decode(result['stdout_base64']), raw)

    def main_observation(self, response, output):
        # Load real authored data before replacing only the synthetic oracle pin
        # and invocation boundary, so preflight provenance is actually exercised.
        loaded = p.load()
        responses = [subprocess.CompletedProcess([], 0, b'pandoc 3.12.1\n', b''), response]
        arguments = ['pandoc_profile.py', '--oracle-executable', str(self.exe),
                     '--oracle-archive', str(self.archive), '--case', 'footnotes-positive',
                     '--oracle-output', str(output)]
        with patch.object(p, 'load', return_value=loaded), \
                patch.object(p, 'ZIP_PIN', p.digest(self.archive.read_bytes())), \
                patch.object(subprocess, 'run', side_effect=responses) as run, \
                patch.object(sys, 'argv', arguments), contextlib.redirect_stderr(io.StringIO()):
            status = p.main()
        return status, run

    def test_main_timeout_retains_partial_bytes_and_identity_read_only(self):
        before = {str(path): (path.read_bytes(), path.stat().st_mtime_ns)
                  for path in p.CORPUS.rglob('*') if path.is_file()}
        output = Path(self.temp.name)/'timeout-observation'
        partial, diagnostic = b'{"blocks":[', b'partial diagnostic\n'
        status, run = self.main_observation(subprocess.TimeoutExpired(['synthetic'], 30,
                                            output=partial, stderr=diagnostic), output)
        self.assertEqual(status, 1)
        record = p.decode((output/'observation.json').read_bytes())
        self.assertFalse(record['valid_observation'])
        self.assertEqual(record['outcome'], 'timeout')
        self.assertIsNone(record['exit'])
        self.assertEqual(record['timeout_seconds'], 30)
        self.assertEqual((output/'stdout').read_bytes(), partial)
        self.assertEqual((output/'stderr').read_bytes(), diagnostic)
        self.assertEqual(base64.b64decode(record['stdout_base64']), partial)
        self.assertEqual(base64.b64decode(record['stderr_base64']), diagnostic)
        source = (p.CORPUS/'inputs/footnotes-positive.md').read_bytes()
        self.assertEqual((output/'input.md').read_bytes(), source)
        self.assertEqual(record['input_sha256'], p.digest(source))
        self.assertEqual(record['executable_sha256'], p.digest(self.exe.read_bytes()))
        self.assertEqual(record['archive_sha256'], p.digest(self.archive.read_bytes()))
        self.assertEqual(record['profile_revision'], self.profile['profile_revision'])
        self.assertEqual(record['reader_argument'], self.profile['reader_argument'])
        self.assertEqual(record['version_stdout'], 'pandoc 3.12.1\n')
        self.assertEqual(record['command'], run.call_args_list[1].args[0])
        after = {str(path): (path.read_bytes(), path.stat().st_mtime_ns)
                 for path in p.CORPUS.rglob('*') if path.is_file()}
        self.assertEqual(before, after)

    def test_main_parseable_malformed_is_nonzero_with_exact_raw_files(self):
        for index, raw in enumerate((b'{"pandoc-api-version":["bogus"],"meta":{},"blocks":[null]}',
                                    b'{"pandoc-api-version":[],"meta":{"a":null},"blocks":[{"t":"NotAPandocNode"}]}')):
            output = Path(self.temp.name)/('malformed-'+str(index))
            status, _ = self.main_observation(subprocess.CompletedProcess([], 0, raw, b''), output)
            self.assertEqual(status, 1)
            self.assertEqual((output/'stdout').read_bytes(), raw)
            record = p.decode((output/'observation.json').read_bytes())
            self.assertFalse(record['valid_observation'])
            self.assertEqual(record['outcome'], 'malformed')
            self.assertEqual(record['exit'], 0)

    def test_wrong_version_and_modified_binary(self):
        for version in (b'pandoc 9.0\n', b''):
            with self.assertRaisesRegex(ValueError, 'version differs'):
                self.synthetic(b'{}', version=version)
        self.exe.write_bytes(b'wrong binary')
        with patch.object(p, 'ZIP_PIN', p.digest(self.archive.read_bytes())), \
                patch.object(subprocess, 'run', side_effect=AssertionError('must not run')):
            with self.assertRaisesRegex(ValueError, 'executable differs'):
                p.run_oracle(self.exe, self.archive, b'x', self.profile)


if __name__ == '__main__':
    unittest.main()
