#!/usr/bin/env python3
"""Check original Pandoc target data, never parser conformance (Python 3.9+)."""

import argparse
import base64
import hashlib
import json
import math
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import zipfile

ROOT = Path(__file__).resolve().parents[1]
CORPUS = ROOT / 'internal/parser/testdata/pandoc/3.12.1'
PROFILE = 'docs/engineering/pandoc-profile.json'
REVISION = 'e51c9c6054c8f4ec5c3209d5abe10939dfe2963e'
PROFILE_PIN = '2b20b9b5bbcaab5e7d816f4d4302c58fb00c4438edc60d27465073a425e55e5b'
ZIP_PIN = 'c2146cf33a583f3ac93eccddf1bb662371d5cfcce1aad1c87ad860cca1435d46'
ARCHIVE_PIN = '1526b4b898de8419ba7b5a46eb4513ebebda81db485ec983a991e63aca806856'
UPSTREAM = {'MANUAL.txt', 'COPYING.md', 'COPYRIGHT',
            'src/Text/Pandoc/Readers/Markdown.hs', 'src/Text/Pandoc/Extensions.hs',
            'test/Tests/Readers/Markdown.hs'}
DIMENSIONS = {'positive', 'fallback', 'escape', 'protected', 'nested', 'crlf',
              'unicode', 'precedence'}
STRUCTURES = {'paragraph', 'heading', 'thematic-break', 'code-block', 'code-span',
              'block-quote', 'list', 'definition-list', 'line-block', 'example-list',
              'div', 'span', 'link', 'image', 'note', 'table', 'strikeout', 'subscript',
              'superscript', 'citation', 'html', 'raw', 'math', 'hard-break',
              'text', 'soft-break', 'task-list'}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def fields(value, keys, label):
    require(type(value) is dict and set(value) == set(keys), label+': missing or unknown fields')


def text(value, label, empty=False):
    require(type(value) is str and (empty or bool(value.strip())), label+': expected nonempty string')


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def serialize(value):
    return (json.dumps(value, ensure_ascii=False, indent=2)+'\n').encode()


def pairs(values):
    result = {}
    for key, value in values:
        require(key not in result, 'duplicate JSON key: '+key)
        result[key] = value
    return result


def decode(raw):
    return json.loads(raw, object_pairs_hook=pairs,
                      parse_constant=lambda value: require(False, 'nonfinite JSON: '+value))


def span(source, value, parent=None):
    require(type(value) is list and len(value) == 3, 'span: expected [start,end,raw]')
    start, end, raw = value
    require(type(start) is int and type(end) is int and 0 <= start <= end <= len(source),
            'span: invalid byte bounds')
    text(raw, 'span raw', empty=True)
    try:
        source[:start].decode('utf-8')
        source[:end].decode('utf-8')
    except UnicodeDecodeError as error:
        raise ValueError('span: scalar interior') from error
    require(source[start:end] == raw.encode(), 'span: original bytes differ')
    if parent is not None:
        require(parent[0] <= start <= end <= parent[1], 'span: outside source owner')
    return start, end


def ordered(source, values, parent=None):
    require(type(values) is list, 'spans: expected array')
    previous = 0
    for value in values:
        a, b = span(source, value, parent)
        require(a >= previous, 'spans: overlap or order')
        previous = b


def source_contract(source, record):
    fields(record, ('envelope', 'content', 'gaps', 'positions'), 'source contract')
    owner = span(source, record['envelope'])
    require(owner == (0, len(source)), 'source: document envelope')
    ordered(source, record['content'], owner)
    ordered(source, record['gaps'], owner)
    segments = sorted(record['content']+record['gaps'])
    previous = 0
    for segment in segments:
        require(segment[0] == previous and segment[1] > segment[0], 'source: content/gap partition')
        previous = segment[1]
    require(previous == len(source), 'source: missing original bytes')
    require(type(record['positions']) is list and bool(record['positions']), 'source: positions required')
    for position in record['positions']:
        fields(position, ('offset', 'line', 'column'), 'position')
        offset = position['offset']
        span(source, [offset, offset, ''])
        require(not (0 < offset < len(source) and source[offset-1:offset+1] == b'\r\n'),
                'position: CRLF interior')
        lines = re.split(r'\r\n|\r|\n', source[:offset].decode())
        require(type(position['line']) is int and type(position['column']) is int and
                position['line'] == len(lines) and position['column'] == len(lines[-1])+1,
                'position: physical scalar coordinates differ')


def validate_notes(source, notes):
    fields(notes, ('definitions', 'references', 'inline', 'inventory'), 'notes')
    definitions = notes['definitions']
    references = notes['references']
    require(type(definitions) is list and type(references) is list, 'notes: arrays required')
    ids, last = {}, {}
    require([d['span'][0] for d in definitions] == sorted(d['span'][0] for d in definitions),
            'note: definition source order')
    for d in definitions:
        fields(d, ('id', 'span', 'label', 'content', 'owner', 'container', 'empty'), 'note definition')
        text(d['id'], 'definition ID')
        require(d['id'] not in ids, 'note: duplicate record ID')
        ids[d['id']] = d
        owner = span(source, d['owner'])
        bounds = span(source, d['span'], owner)
        require(d['container'] in ('document', 'quote', 'list', 'note'), 'note: container kind')
        if d['container'] in ('quote', 'list'):
            require(d['owner'][2].startswith('> ' if d['container'] == 'quote' else '- ') and
                    bounds[0] == owner[0]+2, 'note: physical container prefix/owner')
        if d['container'] == 'note':
            require(any(x['span'] == d['owner'] for x in definitions if x is not d), 'note: nested definition owner')
        span(source, d['label'], bounds)
        ordered(source, d['content'], bounds)
        label = d['label'][2]
        require(label and not re.search(r'[\r\n\t \^\[\]]', label), 'note: invalid label')
        require(d['span'][2].startswith('[^'+label+']:'), 'note: definition marker/label')
        require(type(d['empty']) is bool and d['empty'] ==
                (not ''.join(x[2] for x in d['content']).strip()), 'note: empty content differs')
        last[label] = d['id']
    ordered(source, [r['span'] for r in references])
    for r in references:
        fields(r, ('span', 'label', 'context', 'owner', 'eligible', 'binding'), 'note reference')
        span(source, r['span'], span(source, r['owner']))
        span(source, r['label'], (r['span'][0], r['span'][1]))
        require(r['span'][2] == '[^'+r['label'][2]+']', 'note: reference label spelling')
        require(r['context'] in ('body', 'named-note', 'inline-note', 'code', 'raw', 'escape'), 'note: context')
        eligible = r['context'] in ('body', 'inline-note')
        require(type(r['eligible']) is bool and r['eligible'] == eligible, 'note: reference eligibility differs')
        binding = last.get(r['label'][2]) if eligible else None
        require(r['binding'] == binding, 'note: last exact-label binding differs')
        if r['context'] == 'named-note':
            require(any(d['span'][0] <= r['span'][0] <= r['span'][1] <= d['span'][1]
                        for d in definitions), 'note: named-note source owner')
    for n in notes['inline']:
        fields(n, ('span', 'content', 'empty'), 'inline note')
        owner = span(source, n['span'])
        span(source, n['content'], owner)
        require(n['span'][2] == '^['+n['content'][2]+']', 'inline note: original syntax')
        require(type(n['empty']) is bool and n['empty'] == (not n['content'][2].strip()),
                'inline note: empty content differs')
    # Independent authored inventory anchors every retained record, including
    # unused/duplicate/empty definitions and literal/protected reference tokens.
    fields(notes['inventory'], ('definitions', 'references', 'inline'), 'note inventory')
    for key in ('definitions', 'references', 'inline'):
        if key != 'definitions':
            ordered(source, notes['inventory'][key])
        else:
            for value in notes['inventory'][key]:
                span(source, value)
        require(notes['inventory'][key] == [x['span'] for x in notes[key]],
                'note: lost or extra '+key+' record')


def validate_attributes(source, groups):
    for group in groups:
        fields(group, ('owner', 'span', 'ids', 'inventory', 'effective'), 'attributes')
        owner = span(source, group['owner'])
        bounds = span(source, group['span'], owner)
        ordered(source, group['ids'], bounds)
        ordered(source, group['inventory'], bounds)
        require(group['ids'] == group['inventory'], 'attributes: lost raw occurrence/order')
        require(all(x[2].startswith('#') and len(x[2]) > 1 for x in group['ids']), 'attributes: ID spelling')
        expected = group['ids'][-1][2][1:] if group['ids'] else None
        require(group['effective'] == expected, 'attributes: last effective ID differs')


def validate_tasks(source, tasks):
    for task in tasks:
        fields(task, ('owner', 'list_marker', 'marker', 'separator', 'checked'), 'task')
        owner = span(source, task['owner'])
        for key in ('list_marker', 'marker', 'separator'):
            span(source, task[key], owner)
        require(task['marker'][2] in ('[ ]', '[x]', '[X]'), 'task: marker spelling')
        require(type(task['checked']) is bool and task['checked'] == (task['marker'][2] != '[ ]'),
                'task: checked state differs')
        require(task['list_marker'][1] <= task['marker'][0] and
                task['marker'][1] == task['separator'][0] and
                task['separator'][2] in (' ', '\t'), 'task: marker ownership/spacing')


def validate_html(source, html):
    for item in html:
        fields(item, ('owner', 'opening', 'closing', 'origin', 'opaque', 'payload', 'children'), 'HTML')
        owner = span(source, item['owner'])
        for key in ('opening', 'closing', 'payload'):
            span(source, item[key], owner)
        require(item['origin'] in ('physical-div', 'physical-span', 'raw-html'), 'HTML: physical origin required')
        opening, closing = item['opening'], item['closing']
        require(re.fullmatch(r'<[a-z]+(?: [^>]*)?>', opening[2]) is not None and
                re.fullmatch(r'</[a-z]+>', closing[2]) is not None, 'HTML: physical tag spelling')
        name = re.match(r'<([a-z]+)', opening[2])[1]
        require(closing[2] == '</'+name+'>', 'HTML: tag pair')
        require(item['origin'] == ('physical-div' if name == 'div' else
                                  'physical-span' if name == 'span' else 'raw-html'), 'HTML: origin differs')
        require(type(item['opaque']) is bool and item['opaque'] == (name in ('script', 'style', 'pre', 'textarea')),
                'HTML: opaque payload differs')
        require(item['payload'][0] == opening[1] and item['payload'][1] == closing[0], 'HTML: payload boundaries')
        ordered(source, item['children'], (item['payload'][0], item['payload'][1]))
        require((not item['children']) if item['opaque'] else bool(item['children']), 'HTML: child eligibility differs')


def validate_protection(source, values):
    for value in values:
        fields(value, ('owner', 'opening', 'closing', 'payload', 'kind', 'literal'), 'protection')
        owner = span(source, value['owner'])
        for key in ('opening', 'closing', 'payload'):
            span(source, value[key], owner)
        require(value['kind'] in ('code', 'math', 'raw-tex', 'raw-format', 'citation', 'html-tag', 'attribute'),
                'protection: kind')
        require(value['opening'][1] == value['payload'][0] and
                value['payload'][1] == value['closing'][0] and
                value['opening'][0] == owner[0] and value['closing'][1] == owner[1],
                'protection: exact payload/marker boundaries')
        require(value['literal'] == value['payload'][2], 'protection: missing or altered literal payload')


def validate_table(source, table):
    fields(table, ('owner', 'rows', 'width', 'caption'), 'table')
    owner = span(source, table['owner'])
    require(type(table['width']) is int and table['width'] > 0, 'table: width')
    require(len(table['rows']) >= 2, 'table: header/delimiter required')
    for i, row in enumerate(table['rows']):
        fields(row, ('span', 'role', 'cells', 'separators', 'protected', 'semantic', 'inventory'), 'table row')
        bounds = span(source, row['span'], owner)
        require(row['role'] == ('header' if i == 0 else 'delimiter' if i == 1 else 'body'), 'table: row roles')
        ordered(source, row['cells'], bounds)
        ordered(source, row['inventory'], bounds)
        require(row['cells'] == row['inventory'], 'table: lost original cell')
        ordered(source, row['separators'], bounds)
        ordered(source, row['protected'], bounds)
        require(all(x[2] in ('|', '+') for x in row['separators']), 'table: separator spelling')
        # Source accounting checks the declared cells/separators/protected tokens;
        # it does not scan Markdown for code/math/HTML/TeX recognition.
        for offset, byte in enumerate(source[bounds[0]:bounds[1]], bounds[0]):
            if byte in b'|+':
                require(any(s[0] == offset for s in row['separators']) or
                        any(p[0] <= offset < p[1] for p in row['protected']), 'table: unowned separator candidate')
        raw = source[bounds[0]:bounds[1]].rstrip(b'\r\n')
        require(row['separators'] and row['separators'][0][0] == bounds[0] and
                row['separators'][-1][0] == bounds[0]+len(raw)-1, 'table: explicit outer separators')
        expected = [(a[1], b[0]) for a, b in zip(row['separators'], row['separators'][1:])]
        require([(c[0], c[1]) for c in row['cells']] == expected, 'table: complete original field partition')
        if i == 1:
            require(len(row['cells']) == table['width'] and all(
                re.fullmatch(r'\s*:?-+:?\s*', c[2]) for c in row['cells']), 'table: delimiter-derived width')
        expected_semantic = list(range(min(table['width'], len(row['cells']))))
        expected_semantic += [None] * max(0, table['width']-len(row['cells']))
        require(row['semantic'] == expected_semantic and all(x is None or type(x) is int for x in row['semantic']),
                'table: truncation/padding must reference original cells or spanless null')
    if table['caption'] is not None:
        span(source, table['caption'], owner)
        require(table['caption'][1] == owner[1] and table['caption'][2].startswith('Table:'),
                'table: attached caption extent')


def validate_case(case, source):
    fields(case, ('id', 'extension', 'enabled', 'dimension', 'input', 'input_sha256',
                  'target', 'source', 'assertions', 'facts', 'fact_inventory', 'rationale'), 'case')
    for key in ('id', 'extension', 'dimension', 'input', 'rationale'):
        text(case[key], key)
    require(type(case['enabled']) is bool, 'case: enabled must be boolean')
    require(case['dimension'] in DIMENSIONS | {'excluded', 'loss-sensitive'}, 'case: dimension')
    require(digest(source) == case['input_sha256'], case['id']+': input hash mismatch')
    source.decode('utf-8')
    fields(case['target'], ('structure', 'meaning'), 'target')
    require(type(case['target']['structure']) is list and bool(case['target']['structure']) and
            set(case['target']['structure']) <= STRUCTURES, 'target: explicit structure required')
    text(case['target']['meaning'], 'target meaning')
    source_contract(source, case['source'])
    require(type(case['assertions']) is list and bool(case['assertions']), 'source assertions required')
    for assertion in case['assertions']:
        fields(assertion, ('role', 'span', 'meaning'), 'assertion')
        text(assertion['role'], 'assertion role')
        span(source, assertion['span'])
        text(assertion['meaning'], 'assertion meaning')
    fields(case['facts'], ('notes', 'attributes', 'tasks', 'html', 'protected', 'tables'), 'facts')
    facts = case['facts']
    inventory = case['fact_inventory']
    fields(inventory, ('notes', 'attributes', 'tasks', 'html', 'protected', 'tables'), 'fact inventory')
    require(type(inventory['notes']) is bool and inventory['notes'] == (facts['notes'] is not None),
            'fact inventory: missing note collection')
    for key in ('attributes', 'tasks', 'html', 'protected', 'tables'):
        require(type(facts[key]) is list and type(inventory[key]) is list, 'fact inventory: arrays')
        require(inventory[key] == [x['span'] if key == 'attributes' else x['owner'] for x in facts[key]],
                'fact inventory: missing '+key+' record')
    if facts['notes'] is not None:
        validate_notes(source, facts['notes'])
    for key, validator in (('attributes', validate_attributes), ('tasks', validate_tasks),
                           ('html', validate_html), ('protected', validate_protection)):
        require(type(facts[key]) is list, key+': array required')
        validator(source, facts[key])
    require(type(facts['tables']) is list, 'tables: array required')
    for table in facts['tables']:
        validate_table(source, table)


def validate_profile(raw):
    require(digest(raw) == PROFILE_PIN, 'approved profile byte pin differs')
    profile = decode(raw)
    require(len(profile['enabled']) == len(profile['disabled']) == 37 and
            len(set(profile['enabled']+profile['disabled'])) == 74, 'profile: complete 37/37 inventory')
    require(profile['profile_revision'] == 'pandoc-3.12.1-mdchk-1' and
            profile['source_revision'] == REVISION and profile['reader_columns'] == 80 and
            profile['tab_stop'] == 4, 'profile: revision/settings')
    tokens = re.findall(r'([+-])([a-z_]+)', profile['reader_argument'][len('markdown'):])
    require(len(tokens) == 74 and len({name for _, name in tokens}) == 74 and
            {name: sign == '+' for sign, name in tokens} ==
            {name: name in profile['enabled'] for name in profile['upstream_defaults']}, 'profile: reader switches')
    require(sum(profile['upstream_defaults'].values()) == 47 and
            sorted(name for name, enabled in profile['upstream_defaults'].items()
                   if enabled and name in profile['disabled']) == profile['changed_from_defaults'],
            'profile: default changes')
    return profile


def validate_ownership(data, cases, profile):
    fields(data, ('schema_version', 'extensions'), 'coverage')
    require(type(data['schema_version']) is int and data['schema_version'] == 1, 'coverage schema')
    require(type(data['extensions']) is list and len(data['extensions']) == 74, 'coverage: all 74 required')
    seen = set()
    used = set()
    for row in data['extensions']:
        fields(row, ('extension', 'enabled', 'cases', 'dimensions', 'rationale'), 'coverage row')
        name = row['extension']
        require(name in profile['upstream_defaults'] and name not in seen, 'coverage: unknown/duplicate extension')
        seen.add(name)
        require(type(row['enabled']) is bool and row['enabled'] == (name in profile['enabled']), 'coverage: wrong state')
        require(type(row['cases']) is list and bool(row['cases']) and
                len(set(row['cases'])) == len(row['cases']), 'coverage: empty/duplicate ownership')
        text(row['rationale'], 'coverage rationale')
        fields(row['dimensions'], DIMENSIONS, 'coverage dimensions')
        for identity in row['cases']:
            require(identity in cases, 'coverage: dangling case')
            require(cases[identity]['extension'] == name and cases[identity]['enabled'] == row['enabled'],
                    'coverage: foreign case/state')
            used.add(identity)
        for dimension, disposition in row['dimensions'].items():
            fields(disposition, ('cases', 'not_applicable'), 'dimension')
            refs, reason = disposition['cases'], disposition['not_applicable']
            require(type(refs) is list and len(refs) == len(set(refs)), 'dimension: duplicate refs')
            if refs:
                require(reason is None and set(refs) <= set(row['cases']), 'dimension: dangling case')
                if row['enabled']:
                    require(all(cases[x]['dimension'] == dimension for x in refs), 'dimension: wrong witness')
            else:
                text(reason, 'dimension: explicit N/A rationale required')
    require(seen == set(profile['upstream_defaults']) and used == set(cases), 'coverage: missing/unowned case')


def verify_entry(path, entry):
    fields(entry, ('sha256', 'byte_count', 'origin', 'revision', 'retrieved_at', 'license', 'role'), 'provenance entry')
    for key in ('sha256', 'origin', 'revision', 'retrieved_at', 'license', 'role'):
        text(entry[key], 'provenance '+key)
    raw = path.read_bytes()
    require(type(entry['byte_count']) is int and len(raw) == entry['byte_count'], str(path)+': byte count differs')
    require(digest(raw) == entry['sha256'], str(path)+': SHA-256 differs')
    return raw



def verify_upstream_states(profile):
    """Read only the pinned extension declarations, never a Markdown parser."""
    raw = (CORPUS/'upstream/src/Text/Pandoc/Extensions.hs').read_text(encoding='utf-8')
    defaults = set(re.findall(r'Ext_([a-z_]+)',
        raw.split('pandocExtensions = extensionsFromList', 1)[1].split(']', 1)[0]))
    declarations = raw.split('getAllExtensions f =', 1)[1].split('getAll "markdown_strict"', 1)[0]
    names = defaults | set(re.findall(r'Ext_([a-z_]+)', declarations))
    require(names == set(profile['upstream_defaults']) and len(names) == 74 and len(defaults) == 47,
            'pinned upstream: extension inventory/default declarations differ')
    require({name: name in defaults for name in names} == profile['upstream_defaults'],
            'pinned upstream: default states differ')


def load():
    profile = validate_profile((ROOT/PROFILE).read_bytes())
    provenance = decode((CORPUS/'provenance.json').read_bytes())
    fields(provenance, ('schema_version', 'references', 'authored', 'generated', 'unretrieved', 'research'), 'provenance')
    require(type(provenance['schema_version']) is int and provenance['schema_version'] == 1, 'provenance schema')
    require(set(provenance['references']) == UPSTREAM, 'provenance: complete upstream references/notices required')
    for name, entry in provenance['references'].items():
        require(entry['revision'] == REVISION and entry['license'] == 'GPL-2.0-or-later' and
                entry['origin'] == 'https://raw.githubusercontent.com/jgm/pandoc/'+REVISION+'/'+name,
                'provenance: upstream origin/license differs')
        verify_entry(CORPUS/'upstream'/name, entry)
    require(provenance['references']['MANUAL.txt']['sha256'] ==
            '1d12bfb86e515bff05f56dab8dbfcfa409303cab65224e9fc4a037794ae09e60' and
            provenance['references']['src/Text/Pandoc/Readers/Markdown.hs']['sha256'] ==
            '7c4a62e5f9ddec61823d52e34160d1b2ff6fa14fc925eef672b51e65f393a2c7', 'upstream approved pins')
    verify_upstream_states(profile)
    require(provenance['unretrieved'] == {'source_archive_sha256': ARCHIVE_PIN, 'oracle_zip_sha256': ZIP_PIN,
                                        'status': 'expected pins only; artifact bytes not retained or verified'},
            'provenance: expected versus verified artifact boundary')
    require(provenance['research'] == {'historical_inputs': 99, 'upstream_comparisons': 5,
                                      'status': 'original research bytes unavailable; no execution claimed'},
            'provenance: research boundary')
    expected = {'expectations.json', 'coverage.json', 'tools/pandoc_profile.py', 'tools/test_pandoc_profile.py'}
    data = decode((CORPUS/'expectations.json').read_bytes())
    fields(data, ('schema_version', 'authorship', 'profile_revision', 'cases'), 'expectations')
    require(type(data['schema_version']) is int and data['schema_version'] == 1 and
            data['profile_revision'] == profile['profile_revision'], 'expectations schema/profile')
    text(data['authorship'], 'authorship')
    require(type(data['cases']) is list and bool(data['cases']), 'cases required')
    cases = {}
    for case in data['cases']:
        identity = case['id']
        require(identity not in cases and re.fullmatch(r'[a-z0-9-]+', identity), 'case: duplicate/invalid ID')
        require(case['input'] == 'inputs/'+identity+'.md', 'case: input path identity')
        expected.add(case['input'])
        cases[identity] = case
        try:
            validate_case(case, (CORPUS/case['input']).read_bytes())
        except (ValueError, TypeError, KeyError) as error:
            raise ValueError(identity+': '+str(error)) from error
    require({'inputs/'+x.name for x in (CORPUS/'inputs').iterdir()} ==
            {case['input'] for case in cases.values()}, 'inputs: missing or unexpected files')
    require(set(provenance['authored']) == expected, 'provenance: complete authored input/tool inventory')
    for name, entry in provenance['authored'].items():
        verify_entry(ROOT/name if name.startswith('tools/') else CORPUS/name, entry)
    coverage = decode((CORPUS/'coverage.json').read_bytes())
    validate_ownership(coverage, cases, profile)
    return profile, cases, coverage, provenance


def ledger(profile, cases, coverage):
    return {'schema_version': 1, 'profile_revision': profile['profile_revision'],
            'extensions': coverage['extensions'],
            'cases': [{'id': identity, 'input_sha256': case['input_sha256'], 'target': case['target'],
                       'actual_parser': 'NOT-RUN/unimplemented'} for identity, case in sorted(cases.items())],
            'actual_parser_passes': 0, 'conformance_complete': False}


# Pandoc-types 1.23 JSON constructors (the pinned reader depends on >=1.23.1.2,
# <1.24). This validates serialized observations, not Markdown recognition.
# Shape definitions are original declarations of the public JSON interface.
ORACLE_NODES = {
    'Inline': {
        'Str': 'string', 'Emph': ('array', 'Inline'), 'Underline': ('array', 'Inline'),
        'Strong': ('array', 'Inline'), 'Strikeout': ('array', 'Inline'),
        'Superscript': ('array', 'Inline'), 'Subscript': ('array', 'Inline'),
        'SmallCaps': ('array', 'Inline'), 'Quoted': ('tuple', 'QuoteType', ('array', 'Inline')),
        'Cite': ('tuple', ('array', 'Citation'), ('array', 'Inline')),
        'Code': ('tuple', 'Attr', 'string'), 'Space': None, 'SoftBreak': None, 'LineBreak': None,
        'Math': ('tuple', 'MathType', 'string'), 'RawInline': ('tuple', 'string', 'string'),
        'Link': ('tuple', 'Attr', ('array', 'Inline'), 'Target'),
        'Image': ('tuple', 'Attr', ('array', 'Inline'), 'Target'),
        'Note': ('array', 'Block'), 'Span': ('tuple', 'Attr', ('array', 'Inline')),
    },
    'Block': {
        'Plain': ('array', 'Inline'), 'Para': ('array', 'Inline'),
        'LineBlock': ('array', ('array', 'Inline')), 'CodeBlock': ('tuple', 'Attr', 'string'),
        'RawBlock': ('tuple', 'string', 'string'), 'BlockQuote': ('array', 'Block'),
        'OrderedList': ('tuple', 'ListAttributes', ('array', ('array', 'Block'))),
        'BulletList': ('array', ('array', 'Block')),
        'DefinitionList': ('array', ('tuple', ('array', 'Inline'), ('array', ('array', 'Block')))),
        'Header': ('tuple', 'integer', 'Attr', ('array', 'Inline')), 'HorizontalRule': None,
        'Table': ('tuple', 'Attr', 'Caption', ('array', 'ColSpec'), 'TableHead', ('array', 'TableBody'), 'TableFoot'),
        'Figure': ('tuple', 'Attr', 'Caption', ('array', 'Block')),
        'Div': ('tuple', 'Attr', ('array', 'Block')),
    },
    'MetaValue': {
        'MetaMap': ('map', 'MetaValue'), 'MetaList': ('array', 'MetaValue'),
        'MetaBool': 'boolean', 'MetaString': 'string',
        'MetaInlines': ('array', 'Inline'), 'MetaBlocks': ('array', 'Block'),
    },
    'ColWidth': {'ColWidthDefault': None, 'ColWidth': 'number'},
}
ORACLE_ENUMS = {
    'QuoteType': {'SingleQuote', 'DoubleQuote'}, 'MathType': {'DisplayMath', 'InlineMath'},
    'CitationMode': {'AuthorInText', 'SuppressAuthor', 'NormalCitation'},
    'ListNumberStyle': {'DefaultStyle', 'Example', 'Decimal', 'LowerRoman', 'UpperRoman', 'LowerAlpha', 'UpperAlpha'},
    'ListNumberDelim': {'DefaultDelim', 'Period', 'OneParen', 'TwoParens'},
    'Alignment': {'AlignLeft', 'AlignRight', 'AlignCenter', 'AlignDefault'},
}
ORACLE_PRODUCTS = {
    'Attr': ('tuple', 'string', ('array', 'string'), ('array', ('tuple', 'string', 'string'))),
    'Target': ('tuple', 'string', 'string'),
    'ListAttributes': ('tuple', 'integer', 'ListNumberStyle', 'ListNumberDelim'),
    'Caption': ('tuple', ('maybe', ('array', 'Inline')), ('array', 'Block')),
    'ColSpec': ('tuple', 'Alignment', 'ColWidth'),
    'TableHead': ('tuple', 'Attr', ('array', 'Row')),
    'TableBody': ('tuple', 'Attr', 'integer', ('array', 'Row'), ('array', 'Row')),
    'TableFoot': ('tuple', 'Attr', ('array', 'Row')),
    'Row': ('tuple', 'Attr', ('array', 'Cell')),
    'Cell': ('tuple', 'Attr', 'Alignment', 'integer', 'integer', ('array', 'Block')),
}


def oracle_shape(value, shape, depth=0):
    require(depth <= 128, 'oracle JSON: nesting exceeds observation limit')
    if type(shape) is tuple:
        kind, *arguments = shape
        if kind == 'maybe' and value is None:
            return
        if kind == 'maybe':
            oracle_shape(value, arguments[0], depth+1)
        elif kind == 'map':
            require(type(value) is dict and all(type(k) is str for k in value), 'oracle JSON: metadata map')
            for key, item in value.items():
                key.encode('utf-8')
                oracle_shape(item, arguments[0], depth+1)
        else:
            require(type(value) is list, 'oracle JSON: expected array/product')
            if kind == 'tuple':
                require(len(value) == len(arguments), 'oracle JSON: product arity')
                for item, item_shape in zip(value, arguments):
                    oracle_shape(item, item_shape, depth+1)
            else:
                require(kind == 'array', 'oracle JSON: internal shape')
                for item in value:
                    oracle_shape(item, arguments[0], depth+1)
    elif shape in ORACLE_PRODUCTS:
        oracle_shape(value, ORACLE_PRODUCTS[shape], depth+1)
    elif shape in ORACLE_ENUMS:
        fields(value, ('t',), 'oracle JSON enum')
        require(type(value['t']) is str and value['t'] in ORACLE_ENUMS[shape], 'oracle JSON: enum tag')
    elif shape in ORACLE_NODES:
        require(type(value) is dict and type(value.get('t')) is str and
                value['t'] in ORACLE_NODES[shape], 'oracle JSON: unknown/non-node '+shape)
        payload = ORACLE_NODES[shape][value['t']]
        fields(value, ('t',) if payload is None else ('t', 'c'), 'oracle JSON '+shape)
        if payload is not None:
            oracle_shape(value['c'], payload, depth+1)
    elif shape == 'Citation':
        types = {'citationId': 'string', 'citationPrefix': ('array', 'Inline'),
                 'citationSuffix': ('array', 'Inline'), 'citationMode': 'CitationMode',
                 'citationNoteNum': 'integer', 'citationHash': 'integer'}
        fields(value, types, 'oracle JSON citation')
        for key, item_shape in types.items():
            oracle_shape(value[key], item_shape, depth+1)
    elif shape == 'string':
        text(value, 'oracle JSON string', empty=True)
        value.encode('utf-8')
    elif shape == 'integer':
        require(type(value) is int, 'oracle JSON: expected integer')
    elif shape == 'boolean':
        require(type(value) is bool, 'oracle JSON: expected boolean')
    elif shape == 'number':
        require(type(value) in (int, float) and -sys.float_info.max <= value <= sys.float_info.max and math.isfinite(value), 'oracle JSON: expected finite number')
    else:
        raise ValueError('oracle JSON: unknown internal shape')


def validate_oracle_document(value):
    fields(value, ('pandoc-api-version', 'meta', 'blocks'), 'oracle JSON document')
    version = value['pandoc-api-version']
    require(type(version) is list and len(version) >= 2 and
            all(type(x) is int and x >= 0 for x in version) and version[:2] == [1, 23],
            'oracle JSON: incompatible or malformed API version')
    oracle_shape(value['meta'], ('map', 'MetaValue'))
    oracle_shape(value['blocks'], ('array', 'Block'))


def run_oracle(executable, archive, source, profile):
    """Explicit NEW observation only. Synthetic boundary tests are not oracle runs."""
    require(digest(archive.read_bytes()) == ZIP_PIN, 'oracle: release ZIP digest differs')
    with zipfile.ZipFile(archive) as zipped:
        names = [n for n in zipped.namelist() if n.endswith('/bin/pandoc')]
        require(len(names) == 1, 'oracle: one archived executable required')
        expected_binary = zipped.read(names[0])
    raw_executable = executable.read_bytes()
    require(raw_executable == expected_binary, 'oracle: executable differs from pinned ZIP')
    version = subprocess.run([str(executable), '--version'], capture_output=True, check=True, timeout=20)
    require(version.stdout.splitlines()[:1] == [b'pandoc 3.12.1'], 'oracle: version differs')
    with tempfile.TemporaryDirectory(prefix='mdchk-pandoc-empty-data-') as empty:
        command = [str(executable), '--from='+profile['reader_argument'], '--to=json', '--sandbox',
                   '--data-dir='+empty, '--columns=80', '--tab-stop=4']
        try:
            result = subprocess.run(command, input=source, capture_output=True, check=False, timeout=30)
            stdout, stderr, exit_code = result.stdout, result.stderr, result.returncode
            outcome = 'success' if exit_code == 0 else 'nonzero'
        except subprocess.TimeoutExpired as error:
            stdout, stderr, exit_code = error.stdout or b'', error.stderr or b'', None
            outcome = 'timeout'
    record = {'kind': 'NEW optional oracle observation; not mdchk conformance',
              'profile_revision': profile['profile_revision'], 'reader_argument': profile['reader_argument'],
              'input_sha256': digest(source), 'executable_sha256': digest(raw_executable),
              'archive_sha256': ZIP_PIN, 'version_stdout': version.stdout.decode(),
              'version_stderr': version.stderr.decode(), 'version_exit': version.returncode,
              'command': command, 'stdout_base64': base64.b64encode(stdout).decode(),
              'stderr_base64': base64.b64encode(stderr).decode(),
              'exit': exit_code, 'outcome': outcome, 'timeout_seconds': 30 if outcome == 'timeout' else None,
              'valid_observation': False}
    if outcome == 'success':
        try:
            validate_oracle_document(decode(stdout))
            record['valid_observation'] = True
        except (ValueError, UnicodeError, RecursionError) as error:
            record['outcome'] = 'malformed'
            record['validation_error'] = str(error)
    return record


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='read-only integrity check')
    parser.add_argument('--json', action='store_true')
    parser.add_argument('--require-complete', action='store_true', help='always refuses until real parser exists')
    parser.add_argument('--oracle-executable', type=Path)
    parser.add_argument('--oracle-archive', type=Path)
    parser.add_argument('--oracle-output', type=Path)
    parser.add_argument('--case')
    args = parser.parse_args()
    try:
        profile, cases, coverage, provenance = load()
        output = serialize(ledger(profile, cases, coverage))
        entry = provenance['generated']
        require(set(entry) == {'ledger.json'}, 'provenance: generated inventory')
        require(digest(output) == entry['ledger.json']['sha256'] and
                len(output) == entry['ledger.json']['byte_count'], 'generated ledger digest differs')
        if args.check or args.require_complete:
            verify_entry(CORPUS/'ledger.json', entry['ledger.json'])
            require((CORPUS/'ledger.json').read_bytes() == output, 'ledger drift')
        require(not args.require_complete, 'conformance unavailable: actual Pandoc parser NOT-RUN/unimplemented; zero passes')
        oracle_args = (args.oracle_executable, args.oracle_archive, args.oracle_output, args.case)
        if any(oracle_args):
            require(all(oracle_args) and not args.check, 'oracle: explicit executable/archive/output/case required, separate from --check')
            require(args.case in cases, 'oracle: unknown case')
            require(not args.oracle_output.exists(), 'oracle: output must be a new directory')
            record = run_oracle(args.oracle_executable.resolve(), args.oracle_archive,
                                (CORPUS/cases[args.case]['input']).read_bytes(), profile)
            args.oracle_output.mkdir(parents=True)
            (args.oracle_output/'observation.json').write_bytes(serialize(record))
            (args.oracle_output/'stdout').write_bytes(base64.b64decode(record['stdout_base64']))
            (args.oracle_output/'stderr').write_bytes(base64.b64decode(record['stderr_base64']))
            (args.oracle_output/'input.md').write_bytes((CORPUS/cases[args.case]['input']).read_bytes())
            require(record['valid_observation'], 'oracle: failed/partial output retained, not valid evidence')
        elif not args.check:
            (CORPUS/'ledger.json').write_bytes(output)
        report = {'expected_validity': 'valid', 'cases': len(cases), 'enabled': 37, 'disabled': 37,
                  'actual_parser': 'NOT-RUN/unimplemented', 'actual_parser_passes': 0,
                  'conformance_complete': False, 'oracle': 'not invoked' if not any(oracle_args) else 'NEW observation only'}
        print(json.dumps(report, sort_keys=True) if args.json else
              'Pandoc fixture integrity: '+str(len(cases))+' original targets valid; 74 states owned; '
              'actual parser NOT-RUN/unimplemented; zero passes; conformance_complete=false')
    except (OSError, ValueError, TypeError, KeyError, UnicodeError, subprocess.SubprocessError, zipfile.BadZipFile) as error:
        print('Pandoc profile: '+str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
