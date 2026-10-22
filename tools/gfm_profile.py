#!/usr/bin/env python3
"""Validate approved GFM fixture contracts, separately from consumer conformance.

Only retained specification/example data is read. No parser or comparison renderer
is invoked. Python 3.9+, standard library only.
"""

import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

import gfm_corpus

ROOT = Path(__file__).resolve().parents[1]
PROFILE = ROOT / 'internal/parser/testdata/gfm/profile'
CORE_PIN = '257c41ad946f7a1414a499aca402a1aa8fdac3678532266611348c1cf54f4b80'
SCOPES = ('original', 'core', 'integrated')
KINDS = {'document', 'paragraph', 'text', 'html-block', 'inline-html',
         'table', 'task-item', 'strike', 'link', 'block-quote', 'list', 'list-item',
         'emphasis', 'strong', 'code-span', 'code-block', 'filtered-tag'}
NAMES = {'title', 'textarea', 'style', 'xmp', 'iframe', 'noembed',
         'noframes', 'script', 'plaintext'}
SECTIONS = {'Thematic breaks', 'Code spans', 'Fenced code blocks',
            'Preliminaries', 'HTML blocks', 'Raw HTML',
            'Links', 'Autolinks', 'Entity and numeric character references',
            'Emphasis and strong emphasis', 'Tables (extension)',
            'Task list items (extension)', 'Strikethrough (extension)',
            'Autolinks (extension)', 'Disallowed Raw HTML (extension)'}


def require(ok, message):
    if not ok:
        raise ValueError(message)


def fields(value, expected, label):
    require(type(value) is dict and set(value) == set(expected),
            f'{label}: missing or unknown fields')


def string(value, label, empty=False):
    require(type(value) is str and (empty or value != ''), f'{label}: expected string')


def integer(value, label):
    require(type(value) is int and value >= 0, f'{label}: expected nonnegative integer')


def array(value, label):
    require(type(value) is list, f'{label}: expected array')


def boolean(value, label):
    require(type(value) is bool, f'{label}: expected boolean')


def digest(data):
    return hashlib.sha256(data).hexdigest()


def source_span(source, value, parent=None):
    require(type(value) is list and len(value) == 3, 'span: expected [start,end,raw]')
    start, end, raw = value
    integer(start, 'span start')
    integer(end, 'span end')
    string(raw, 'span raw', empty=True)
    b = source.encode('utf-8')
    require(start <= end <= len(b), 'span: reversed or out of bounds')
    try:
        b[:start].decode('utf-8')
        b[:end].decode('utf-8')
    except UnicodeDecodeError as error:
        raise ValueError('span: offset is not a UTF-8 boundary') from error
    require(b[start:end] == raw.encode('utf-8'), 'span: original raw bytes differ')
    if parent is not None:
        require(parent[0] <= start <= end <= parent[1], 'span: outside owning envelope')
    return start, end


def spans(source, values, parent):
    array(values, 'source segments')
    previous = parent[0]
    for value in values:
        start, end = source_span(source, value, parent)
        require(start >= previous, 'source segments: overlap or out of order')
        previous = end


def positions(source, values):
    array(values, 'positions')
    for value in values:
        fields(value, ('offset', 'line', 'column'), 'position')
        for key in value:
            integer(value[key], 'position '+key)
        source_span(source, [value['offset'], value['offset'], ''])
        before = source.encode()[:value['offset']].decode()
        # CRLF is one ending; positions count scalar characters, including tabs.
        lines = re.split(r'\r\n|\r|\n', before)
        require(value['line'] == len(lines) and value['column'] == len(lines[-1])+1,
                'position: expected one-based scalar coordinates (tab counts one)')


def table_pipe_offsets(raw):
    """Original byte offsets under the approved odd/even run interpretation.

    GFM 4.10 requires escaped cell pipes but does not explicitly order longer
    backslash runs. mdchk chooses core punctuation-escape grouping: pairs escape
    each other, and an odd remaining backslash escapes the pipe. This is a
    recorded compatibility interpretation, not an unequivocal upstream mandate.
    """
    separators, escapes = [], []
    slashes = 0
    for offset, byte in enumerate(raw):
        if byte == ord('|'):
            if slashes % 2:
                escapes.append(offset-1)
            else:
                separators.append(offset)
        slashes = slashes+1 if byte == ord('\\') else 0
    return separators, escapes


def check_table(source, envelope, facts):
    fields(facts, ('rows', 'width'), 'table facts')
    integer(facts['width'], 'table width')
    require(facts['width'] > 0, 'table: empty width')
    rows = facts['rows']
    array(rows, 'table rows')
    require(len(rows) >= 2, 'table: header and delimiter required')
    previous = envelope[0]
    for i, row in enumerate(rows):
        fields(row, ('role', 'span', 'cells', 'source_count', 'semantic_count',
                     'alignment', 'separators', 'outer', 'padding', 'escaped_pipes'), 'table row')
        bounds = source_span(source, row['span'], envelope)
        require(bounds[0] >= previous, 'table: row source order')
        previous = bounds[1]
        require(row['role'] == ('header' if i == 0 else 'delimiter' if i == 1 else 'body'),
                'table: header/delimiter/body roles')
        array(row['cells'], 'source cells')
        integer(row['source_count'], 'source count')
        require(row['source_count'] == len(row['cells']), 'table: source cell count differs')
        integer(row['semantic_count'], 'semantic count')
        require(row['semantic_count'] == facts['width'], 'table: semantic width differs')
        if i < 2:
            require(row['source_count'] == facts['width'], 'table: header/delimiter width mismatch')
        cell_previous = bounds[0]
        for cell in row['cells']:
            fields(cell, ('span', 'value', 'padding'), 'source cell')
            cb = source_span(source, cell['span'], bounds)
            require(cb[0] >= cell_previous, 'table: cell source order')
            cell_previous = cb[1]
            require(cell['padding'] is False, 'table: source cell cannot be semantic padding')
            string(cell['value'], 'cell semantic value', empty=True)
            require(cell['value'] == cell['span'][2].strip(' \t').replace('\\|', '|'),
                    'table: cell escaped-pipe semantic value differs')
        array(row['alignment'], 'alignment')
        require(len(row['alignment']) == facts['width'] and
                all(x in ('none', 'left', 'right', 'center') for x in row['alignment']),
                'table: invalid alignment')
        spans(source, row['separators'], bounds)
        structural, escaped = table_pipe_offsets(row['span'][2].encode())
        expected = [bounds[0]+offset for offset in structural]
        require([x[0] for x in row['separators']] == expected and
                all(x[2] == '|' for x in row['separators']), 'table: actual separator inventory differs')
        require(type(row['outer']) is list and len(row['outer']) == 2 and
                all(type(x) is bool for x in row['outer']), 'table: outer pipe flags')
        raw_bytes = row['span'][2].encode().rstrip(b'\r\n')
        first, last = 0, len(structural)
        leading = bool(structural and structural[0] == len(raw_bytes)-len(raw_bytes.lstrip(b' \t')))
        if leading:
            first += 1
        trailing = first < last and structural[-1] == len(raw_bytes.rstrip(b' \t'))-1
        if trailing:
            last -= 1
        require(row['outer'] == [leading, trailing], 'table: outer pipe spelling')
        # Original cell fields exhaust the row between actual separators,
        # including whitespace and every excess source cell.
        left = bounds[0] + (structural[0]+1 if leading else 0)
        right = bounds[0] + (structural[-1] if trailing else len(raw_bytes))
        interior = [x[0] for x in row['separators'] if left <= x[0] < right]
        starts, ends = [left]+[x+1 for x in interior], interior+[right]
        require([(x['span'][0], x['span'][1]) for x in row['cells']] == list(zip(starts, ends)),
                'table: raw source cells must exhaust all fields including excess cells')
        array(row['padding'], 'padding')
        require(len(row['padding']) == max(0, facts['width']-row['source_count']),
                'table: missing or extra semantic padding')
        for padding in row['padding']:
            fields(padding, ('span', 'value', 'padding'), 'semantic padding')
            require(padding == {'span': None, 'value': '', 'padding': True},
                    'table: semantic padding must be spanless')
        spans(source, row['escaped_pipes'], bounds)
        expected = [bounds[0]+offset for offset in escaped]
        require([x[0] for x in row['escaped_pipes']] == expected and
                all(x[2] == '\\|' for x in row['escaped_pipes']), 'table: escaped-pipe inventory differs')
    for column, cell in enumerate(rows[1]['cells']):
        spelling = cell['span'][2].strip(' \t')
        require(re.fullmatch(r':?-+:?', spelling) is not None, 'table: delimiter grammar')
        alignment = ('center' if spelling.startswith(':') and spelling.endswith(':') else
                     'left' if spelling.startswith(':') else 'right' if spelling.endswith(':') else 'none')
        require(all(row['alignment'][column] == alignment for row in rows),
                'table: delimiter alignment differs')


def check_task(source, envelope, facts):
    fields(facts, ('item', 'paragraph', 'marker', 'check', 'separator', 'checked',
                   'list_marker', 'tight') +
           (('continuation_prefixes',) if 'continuation_prefixes' in facts else ()), 'task facts')
    item = source_span(source, facts['item'], envelope)
    require(item == envelope, 'task: item must equal the whole owning node envelope')
    para = source_span(source, facts['paragraph'], item)
    marker = source_span(source, facts['marker'], para)
    source_span(source, facts['check'], marker)
    source_span(source, facts['separator'], para)
    list_marker = source_span(source, facts['list_marker'], item)
    require(list_marker[0] == item[0] and list_marker[1] == para[0],
            'task: owning item must start at its own list marker, not container indentation')
    if 'continuation_prefixes' in facts:
        spans(source, facts['continuation_prefixes'], item)
        require(all(x[2] and x[2].strip(' \t') == '' for x in facts['continuation_prefixes']),
                'task: continuation prefixes must preserve original indentation')
    boolean(facts['checked'], 'task checked')
    boolean(facts['tight'], 'list tight')
    require(facts['marker'][2] in ('[ ]', '[x]', '[X]'), 'task: invalid marker')
    require(facts['check'] == [marker[0]+1, marker[0]+2, facts['marker'][2][1]],
            'task: original check spelling/range differs')
    require(facts['checked'] == (facts['check'][2] in 'xX'), 'task: checked state differs')
    require(marker[0] == para[0] and facts['separator'][0] == marker[1] and
            facts['separator'][2] in (' ', '\t', '\r\n', '\n', '\r'),
            'task: first paragraph marker/separator association')
    require(facts['list_marker'][2] in ('- ', '+ ', '* '), 'task: list marker association')


def check_strike(source, envelope, facts):
    fields(facts, ('opening', 'closing', 'arity'), 'strike facts')
    require(type(facts['arity']) is int and facts['arity'] in (1, 2), 'strike: arity must be one or two')
    opening = source_span(source, facts['opening'], envelope)
    closing = source_span(source, facts['closing'], envelope)
    require(opening[0] == envelope[0] and closing[1] == envelope[1] and opening[1] <= closing[0],
            'strike: delimiter envelope/order')
    require(facts['opening'][2] == facts['closing'][2] == '~'*facts['arity'],
            'strike: delimiter arity/spelling differs')
    require('\n\n' not in source.encode()[opening[1]:closing[0]].decode(),
            'strike: cannot cross paragraph boundary')


def check_link(source, envelope, facts):
    fields(facts, ('origin', 'visible', 'destination', 'parts') +
           (('positions',) if 'positions' in facts else ()) +
           (('binding',) if 'binding' in facts else ()), 'link facts')
    require(facts['origin'] in ('bare-gfm', 'angle', 'inline', 'reference'), 'link: source origin')
    source_span(source, facts['visible'], envelope)
    string(facts['destination'], 'link destination')
    array(facts['parts'], 'destination provenance')
    value = ''
    for part in facts['parts']:
        fields(part, ('source', 'value', 'transformation'), 'destination component')
        string(part['value'], 'component value')
        if part['transformation'] == 'synthetic':
            require(part['source'] is None and part['value'] in ('http://', 'mailto:'),
                    'link: injected prefix must be spanless')
        elif part['transformation'] == 'reference':
            require(part['source'] is None and facts['origin'] == 'reference',
                    'link: projected reference destination must be spanless')
        else:
            require(part['transformation'] == 'exact', 'link: unknown provenance transformation')
            source_span(source, part['source'], envelope)
            require(part['source'][2] == part['value'], 'link: exact destination bytes differ')
        value += part['value']
    require(value == facts['destination'], 'link: destination provenance does not concatenate')
    if facts['origin'] == 'reference':
        require('binding' in facts, 'reference: missing separate definition binding')
        binding = facts['binding']
        fields(binding, ('definition', 'label', 'destination', 'normalized_label'), 'reference binding')
        definition = source_span(source, binding['definition'])
        source_span(source, binding['label'], definition)
        source_span(source, binding['destination'], definition)
        string(binding['normalized_label'], 'normalized reference label')
        require(not (definition[0] < envelope[1] and envelope[0] < definition[1]),
                'reference: definition must stay separate from the use')
        require(binding['label'][2].casefold() == facts['visible'][2].casefold() == binding['normalized_label'],
                'reference: case-folded use/definition binding differs')
        require(binding['destination'][2] == facts['destination'], 'reference: definition destination differs')
    else:
        require('binding' not in facts, 'reference binding requires reference origin')
    if 'positions' in facts:
        positions(source, facts['positions'])


def check_filter(source, envelope, facts):
    fields(facts, ('name', 'affected', 'protected', 'closing'), 'filtered tag facts')
    require(facts['name'] in NAMES, 'filter: unknown disallowed name')
    boolean(facts['closing'], 'filter closing')
    affected = source_span(source, facts['affected'], envelope)
    protected = source_span(source, facts['protected'])
    require(protected[0] <= envelope[0] <= envelope[1] <= protected[1], 'filter: outside protected HTML')
    raw = source.encode()[envelope[0]:envelope[1]].decode()
    match = re.match(r'<(/?)([a-zA-Z]+)(?=[\t\n\r />])', raw)
    require(match is not None and match[2].lower() == facts['name'] and
            bool(match[1]) == facts['closing'], 'filter: exact tag name/boundary differs')
    require(affected == (envelope[0], envelope[0]+1) and facts['affected'][2] == '<',
            'filter: affected delimiter differs')


def check_node(source, value, parent=None, path='node'):
    try:
        _check_node(source, value, parent)
    except (ValueError, KeyError, TypeError) as error:
        raise ValueError(f'{path}: {error}') from error


def _check_node(source, value, parent):
    fields(value, ('kind', 'span', 'content', 'children', 'facts'), 'node')
    require(value['kind'] in KINDS, 'node: unknown kind')
    envelope = source_span(source, value['span'], parent)
    spans(source, value['content'], envelope)
    array(value['children'], 'ordered children')
    prior = envelope[0]
    for child_index, child in enumerate(value['children']):
        require(child['span'][0] >= prior, 'node: child source order')
        check_node(source, child, envelope, path=f'children[{child_index}]')
        prior = child['span'][1]
    facts = value['facts']
    kind = value['kind']
    if kind == 'paragraph':
        raw = value['span'][2]
        require(re.search(r'\n[ \t]*\n', raw.replace('\r\n', '\n').replace('\r', '\n')) is None,
                'paragraph: envelope cannot cross a blank line or include a separate definition')
        terminal = 2 if raw.endswith('\r\n') else 1 if raw.endswith(('\r', '\n')) else 0
        require(all(part[1] <= envelope[1]-terminal for part in value['content']),
                'paragraph: logical content must exclude the terminal block ending')
    if kind in ('document', 'block-quote', 'list', 'list-item', 'task-item'):
        require(value['content'] == [], 'container: payload belongs to child syntax, not container content')
    if kind == 'table':
        check_table(source, envelope, facts)
    elif kind == 'task-item':
        check_task(source, envelope, facts)
    elif kind == 'strike':
        check_strike(source, envelope, facts)
    elif kind == 'strong':
        fields(facts, ('opening', 'closing', 'arity'), 'strong facts')
        require(facts['arity'] == 2, 'strong: arity differs')
        for key in ('opening', 'closing'):
            source_span(source, facts[key], envelope)
        require(facts['opening'][2] == facts['closing'][2] and facts['opening'][2] in ('**', '__'), 'strong: delimiter spelling differs')
        require(facts['opening'][0] == envelope[0] and facts['closing'][1] == envelope[1], 'strong: envelope differs')
    elif kind == 'link':
        check_link(source, envelope, facts)
    elif kind == 'filtered-tag':
        check_filter(source, envelope, facts)
    elif kind == 'text':
        fields(facts, ('value',) + (('parts',) if 'parts' in facts else ()), 'text facts')
        string(facts['value'], 'text semantic value', empty=True)
        require('\r' not in value['span'][2] and '\n' not in value['span'][2],
                'text: physical line endings belong to break/block syntax, not Text')
        if 'parts' in facts:
            array(facts['parts'], 'text provenance')
            collected = ''
            previous = envelope[0]
            for part in facts['parts']:
                fields(part, ('source', 'value', 'transformation'), 'text component')
                bounds = source_span(source, part['source'], envelope)
                require(bounds[0] == previous, 'text: discontinuous provenance')
                previous = bounds[1]
                raw = part['source'][2]
                if part['transformation'] == 'exact':
                    require(raw == part['value'], 'text: exact semantic value differs')
                elif part['transformation'] == 'named-entity':
                    require(raw == '&amp;' and part['value'] == '&', 'text: named entity semantic value differs')
                elif part['transformation'] == 'numeric-entity':
                    match = re.fullmatch(r'&#(?:(?P<hex>[xX][0-9a-fA-F]{1,6})|(?P<decimal>[0-9]{1,7}));', raw)
                    require(match is not None, 'text: invalid numeric reference spelling')
                    scalar = int(match['hex'][1:], 16) if match['hex'] else int(match['decimal'])
                    semantic = '\ufffd' if scalar == 0 or scalar > 0x10ffff or 0xd800 <= scalar <= 0xdfff else chr(scalar)
                    require(part['value'] == semantic, 'text: numeric entity must preserve the Unicode scalar without browser remapping')
                else:
                    require(part['transformation'] == 'escape' and len(raw) == 2 and
                            raw[0] == '\\' and raw[1] == part['value'], 'text: escaped semantic value differs')
                collected += part['value']
            require(previous == envelope[1] and collected == facts['value'], 'text: provenance coverage/value differs')
    elif kind == 'block-quote':
        fields(facts, ('prefixes',) + (('positions',) if 'positions' in facts else ()), 'quote facts')
        spans(source, facts['prefixes'], envelope)
        require(all(x[2].startswith('>') for x in facts['prefixes']), 'quote: original prefixes')
        if 'positions' in facts:
            positions(source, facts['positions'])
    elif kind == 'list':
        fields(facts, ('tight',), 'list facts')
        boolean(facts['tight'], 'list tight')
    elif kind in ('html-block', 'inline-html', 'code-span', 'code-block'):
        fields(facts, ('protected',) + (('literal', 'normalization', 'opening', 'closing') if 'literal' in facts else ()), 'protected facts')
        require(facts['protected'] is True, 'protected: raw/code payload must stay protected')
        if kind == 'code-block':
            raw = value['span'][2]
            opening = re.match(r' {0,3}(`{3,}|~{3,})[^\r\n]*(?:\r\n|\r|\n)', raw)
            if opening:
                marker = opening[1][0]
                closing = re.search(r'(?m)^ {0,3}'+re.escape(marker)+'{'+str(len(opening[1]))+r',}[ \t]*(?:\r?$|$)', raw[opening.end():])
                start = envelope[0] + len(raw[:opening.end()].encode())
                end = envelope[1] if closing is None else start + len(raw[opening.end():opening.end()+closing.start()].encode())
                require(all(start <= part[0] <= part[1] <= end for part in value['content']),
                        'code: payload source must exclude opening and closing fences')
        if 'literal' in facts:
            require(kind == 'code-span', 'code literal facts require code span')
            opening = source_span(source, facts['opening'], envelope)
            closing = source_span(source, facts['closing'], envelope)
            require(opening[0] == envelope[0] and closing[1] == envelope[1] and
                    facts['opening'][2] == facts['closing'][2] == '`', 'code: original delimiters differ')
            raw = ''.join(x[2] for x in value['content'])
            require(facts['normalization'] in ('none', 'table-escaped-pipe'), 'code: unknown normalization')
            literal = raw.replace('\\|', '|') if facts['normalization'] == 'table-escaped-pipe' else raw
            require(facts['literal'] == literal, 'code: semantic literal differs from declared transformation')
    elif kind == 'paragraph' and facts:
        fields(facts, tuple(k for k in ('value', 'ending') if k in facts), 'paragraph facts')
        if 'value' in facts:
            string(facts['value'], 'paragraph value', empty=True)
        if 'ending' in facts:
            ending = source_span(source, facts['ending'], envelope)
            expected_ending = '\r\n' if value['span'][2].endswith('\r\n') else value['span'][2][-1:]
            require(expected_ending in ('\n', '\r') or expected_ending == '\r\n', 'paragraph: source has no terminal ending')
            require(facts['ending'][2] == expected_ending and ending[1] == envelope[1],
                    'paragraph: ending is separate original block-ending metadata')
    else:
        fields(facts, (), 'container facts')


def check_filter_owners(source, nodes):
    def walk(values, path='nodes'):
        for index, node in enumerate(values):
            location = f'{path}[{index}]'
            yield node, location
            yield from walk(node['children'], location+'.children')
    all_nodes = list(walk(nodes))
    owners = [n for n, _ in all_nodes if n['kind'] in ('html-block', 'inline-html')]
    for node, location in all_nodes:
        if node['kind'] != 'filtered-tag':
            continue
        protected = node['facts']['protected']
        matches = [owner for owner in owners if owner['span'] == protected]
        require(len(matches) == 1, location+': filter: protected range must equal one asserted raw HTML owner')
        owner = matches[0]
        if owner['kind'] == 'inline-html':
            require(owner['span'] == node['span'], location+': filter: inline protection must equal the actual tag construct')
        else:
            raw = owner['span'][2]
            # A literal raw block closes at the end of the first closing-tag
            # line, retaining text on that line but never following prose.
            if re.match(r'<(?:script|style|textarea|pre)(?=[ \t>\r\n])', raw, re.I):
                closing = re.search(r'</(?:script|style|textarea|pre)>', raw, re.I)
                if closing is not None:
                    tail = raw[closing.end():]
                    ending = re.search(r'\r\n|\r|\n', tail)
                    expected_end = closing.end() + (ending.end() if ending else len(tail))
                    require(expected_end == len(raw), location+': filter: raw HTML owner must stop at its closing-tag line')


def validate_fixtures(data):
    fields(data, ('schema_version', 'attribution', 'fixtures'), 'expectation file')
    require(type(data['schema_version']) is int and data['schema_version'] == 1, 'schema version')
    string(data['attribution'], 'attribution')
    array(data['fixtures'], 'fixtures')
    fixtures = {}
    for fixture in data['fixtures']:
        scope = '<metadata>'
        try:
            fields(fixture, ('id', 'source', 'input_sha256', 'reason', 'spec', 'frontmatter',
                             'core', 'integrated'), 'fixture')
            string(fixture['id'], 'fixture ID')
            require(fixture['id'] not in fixtures, 'duplicate fixture ID')
            fixtures[fixture['id']] = fixture
            string(fixture['source'], 'source')
            require(digest(fixture['source'].encode()) == fixture['input_sha256'], 'fixture: input hash mismatch')
            string(fixture['reason'], 'adaptation reason')
            fields(fixture['spec'], ('pin', 'section'), 'governing spec')
            require(fixture['spec']['pin'] in (CORE_PIN, gfm_corpus.SNAPSHOT) and
                    fixture['spec']['section'] in SECTIONS, 'governing spec pin/section')
            require(fixture['frontmatter'] is False, 'frontmatter must remain independently disabled')
            for scope in ('core', 'integrated'):
                fields(fixture[scope], ('html', 'nodes'), 'scope expectation')
                string(fixture[scope]['html'], 'expected HTML', empty=True)
                array(fixture[scope]['nodes'], 'source expectations')
                require(len(fixture[scope]['nodes']) > 0, 'source expectations cannot be absent')
                previous_start = 0
                for node_index, n in enumerate(fixture[scope]['nodes']):
                    check_node(fixture['source'], n, path=f'nodes[{node_index}]')
                    require(n['span'][0] >= previous_start, 'projection: node source order')
                    previous_start = n['span'][0]
                check_filter_owners(fixture['source'], fixture[scope]['nodes'])
        except (ValueError, KeyError, TypeError) as error:
            identity = fixture.get('id', '<missing-id>') if type(fixture) is dict else '<invalid-fixture>'
            raise ValueError(f'expectations.json: {identity} {scope}: {error}') from error
    return fixtures


def serialize(data):
    return (json.dumps(data, ensure_ascii=False, indent=2)+'\n').encode()


def ledger(originals, fixtures):
    records = []
    for original in originals:
        number = original['example']
        reference = 'published-'+str(number)
        record = {key: original[key] for key in ('example', 'snapshot_sha256', 'input_sha256', 'section', 'scope')}
        record['expectations'] = {'original': {'original': number}}
        for scope in ('core', 'integrated'):
            record['expectations'][scope] = ({'fixture': reference, 'scope': scope}
                                              if reference in fixtures else {'original': number})
        records.append(record)
    return {'schema_version': 1, 'extensions': sorted(set(gfm_corpus.EXTENSIONS.values())), 'cases': records}


def validate_ledger(data, originals, fixtures):
    fields(data, ('schema_version', 'extensions', 'cases'), 'ledger')
    require(type(data['schema_version']) is int and data['schema_version'] == 1, 'ledger schema version')
    require(data['extensions'] == sorted(set(gfm_corpus.EXTENSIONS.values())), 'unknown or missing extensions')
    array(data['cases'], 'ledger cases')
    require([x.get('example') for x in data['cases']] == list(range(1, 678)),
            'ledger: missing, duplicate, foreign or unordered case IDs')
    for entry, original in zip(data['cases'], originals):
        fields(entry, ('example', 'snapshot_sha256', 'input_sha256', 'section', 'scope', 'expectations'), 'case')
        for key in ('example', 'snapshot_sha256', 'input_sha256', 'section', 'scope'):
            require(type(entry[key]) is type(original[key]) and entry[key] == original[key],
                    'ledger: original identity/hash/section/scope mismatch')
        fields(entry['expectations'], SCOPES, 'interpretation scopes')
        for scope, ref in entry['expectations'].items():
            if type(ref) is dict and 'original' in ref:
                fields(ref, ('original',), 'original reference')
                require(type(ref['original']) is int and ref['original'] == original['example'],
                        'dangling or foreign original reference')
            else:
                fields(ref, ('fixture', 'scope'), 'adaptation reference')
                require(scope != 'original' and ref['scope'] == scope and ref['fixture'] in fixtures,
                        'dangling assertion reference or wrong scope')
                fixture = fixtures[ref['fixture']]
                require(fixture['source'] == original['markdown'] and fixture['input_sha256'] == original['input_sha256'],
                        'adaptation does not address original input')
    # A reference must resolve exactly to the approved authored scope disposition.
    require(data == ledger(originals, fixtures), 'ledger: scope disposition differs from authored expectations')


def resolve(entry, scope, originals, fixtures):
    ref = entry['expectations'][scope]
    if 'original' in ref:
        return {'html': originals[ref['original']-1]['html'], 'nodes': []}
    return fixtures[ref['fixture']][scope]


def consumer_result(expected, actual):
    """Compare an actual consumer projection; expected validity is not execution."""
    fields(actual, ('status', 'html', 'nodes', 'error'), 'actual consumer result')
    require(actual['status'] in ('pass', 'fail', 'error', 'not-run', 'unimplemented'), 'consumer status')
    if actual['status'] in ('not-run', 'unimplemented'):
        require(actual['html'] is None and actual['nodes'] is None and actual['error'] is None,
                'unimplemented consumer cannot claim produced output')
        return actual['status']
    if actual['status'] == 'error':
        string(actual['error'], 'consumer error')
        return 'error'
    require(actual['error'] is None and type(actual['html']) is str and type(actual['nodes']) is list,
            'consumer: invalid execution output')
    matched = actual['html'] == expected['html'] and actual['nodes'] == expected['nodes']
    require(actual['status'] != 'pass' or matched, 'consumer false pass: HTML or source structure mismatch')
    return 'pass' if matched and actual['status'] == 'pass' else 'fail'


def results(data, originals, fixtures, actual=None, selected=None, require_complete=False):
    """Full accounting before selection. Unexecuted expectations always stay not-run."""
    validate_ledger(data, originals, fixtures)
    expected = {}
    for entry in data['cases']:
        for scope in SCOPES:
            expected[f"published:{entry['example']}:{scope}"] = resolve(entry, scope, originals, fixtures)
    for identity, fixture in fixtures.items():
        if not identity.startswith('published-'):
            for scope in ('core', 'integrated'):
                expected[f'{identity}:{scope}'] = fixture[scope]
    if actual is None:
        actual = {}
    require(type(actual) is dict and set(actual) <= set(expected), 'consumer: unknown case/scope')
    requested = set(expected) if selected is None else set(selected)
    require(requested <= set(expected), 'consumer: unknown selection')
    output = {}
    for identity in expected:
        if identity in requested:
            output[identity] = consumer_result(expected[identity], actual.get(identity, {
                'status': 'not-run', 'html': None, 'nodes': None, 'error': None}))
    if require_complete:
        require(requested == set(expected), 'require-complete: subset cannot establish full profile')
        require(set(actual) == set(expected), 'require-complete: missing actual case/scope')
        require(all(x == 'pass' for x in output.values()), 'require-complete: not-run, unimplemented, mismatch or error')
    return output


def verify_profile_artifact(name):
    provenance = json.loads((PROFILE/'provenance.json').read_bytes())
    fields(provenance, ('schema_version', 'sources', 'artifacts'), 'profile provenance')
    require(provenance['schema_version'] == 1 and type(provenance['schema_version']) is int,
            'profile provenance schema')
    require(provenance['sources'] == [gfm_corpus.SNAPSHOT, CORE_PIN], 'profile provenance source pins')
    require(set(provenance['artifacts']) == {'expectations.json', 'ledger.json', 'tools/gfm_profile.py'},
            'profile provenance artifact inventory')
    for entry in provenance['artifacts'].values():
        fields(entry, ('sha256', 'byte_count', 'license', 'origin', 'role', 'modifications'), 'artifact provenance')
        for key in ('sha256', 'license', 'origin', 'role', 'modifications'):
            string(entry[key], 'artifact '+key)
        integer(entry['byte_count'], 'artifact byte count')
    entry = provenance['artifacts'][name]
    path = ROOT/name if name.startswith('tools/') else PROFILE/name
    content = path.read_bytes()
    require(len(content) == entry['byte_count'] and digest(content) == entry['sha256'],
            'profile artifact digest differs: '+name)


def load():
    # Preserve issue227 integrity, including the immutable original HTML bytes.
    regenerated = gfm_corpus.generate()
    require(regenerated == (gfm_corpus.CORPUS/'examples.json').read_bytes(), 'immutable original corpus drift')
    originals = json.loads(regenerated)
    core = ROOT/'internal/parser/testdata/commonmark/0.31.2/upstream/spec.txt'
    require(digest(core.read_bytes()) == CORE_PIN, 'CommonMark governing source pin mismatch')
    data = json.loads((PROFILE/'expectations.json').read_bytes())
    fixtures = validate_fixtures(data)
    verify_profile_artifact('expectations.json')
    verify_profile_artifact('tools/gfm_profile.py')
    return originals, fixtures


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='read-only ledger validation')
    parser.add_argument('--require-complete', action='store_true', help='require all actual consumers; currently refuses')
    parser.add_argument('--json', action='store_true', help='machine-readable validity and actual statuses')
    args = parser.parse_args()
    try:
        originals, fixtures = load()
        derived = ledger(originals, fixtures)
        output = serialize(derived)
        require(output == serialize(ledger(originals, fixtures)), 'nondeterministic ledger')
        path = PROFILE/'ledger.json'
        if args.check or args.require_complete:
            data = json.loads(path.read_bytes())
            validate_ledger(data, originals, fixtures)
            require(path.read_bytes() == output, 'ledger bytes drift; regenerate with tools/gfm_profile.py')
            verify_profile_artifact('ledger.json')
        else:
            path.write_bytes(output)
        actual = results(derived, originals, fixtures)
        report = {'expected_validity': 'valid', 'published_cases': 677,
                  'scopes': list(SCOPES), 'source_contract_fixtures': len(fixtures),
                  'published_consumer_units': 677*3,
                  'supplemental_consumer_units': 2*sum(not x.startswith('published-') for x in fixtures),
                  'actual_consumers': actual, 'conformance_complete': False}
        if args.json:
            print(json.dumps(report, sort_keys=True))
        else:
            print(f'GFM profile fixture integrity: 677 original/core/integrated expectations; '
                  f'{len(fixtures)} source contracts valid; actual consumers: '
                  f'{len(actual)} NOT-RUN (no GFM consumer integration)')
        if args.require_complete:
            results(derived, originals, fixtures, require_complete=True)
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f'GFM profile: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
