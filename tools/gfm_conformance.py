#!/usr/bin/env python3
"""Execute retained approved GFM expectations against actual parser facts, offline.

This is test infrastructure. Raw historical equality and approved modern-target
conformance are reported independently; neither announces production support.
"""
import argparse
from collections import Counter
import difflib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import signal
import threading

import gfm_profile as profile

SECTION_EXECUTIONS = {'Tables (extension)': 'tables-only',
                      'Task list items (extension)': 'tasks-only',
                      'Strikethrough (extension)': 'strike-only',
                      'Autolinks (extension)': 'bare-only',
                      'Disallowed Raw HTML (extension)': 'filter-only'}
MAX_RESPONSE = 16 << 20
MAX_RETAINED_ACTUAL = 32 << 20
ACTUAL_KINDS = profile.KINDS | {'heading', 'thematic-break', 'soft-break', 'hard-break', 'image', 'table-row', 'table-cell'}


def inventory(data, originals, fixtures):
    profile.validate_ledger(data, originals, fixtures)
    units = []
    for entry, original in zip(data['cases'], originals):
        for scope in profile.SCOPES:
            target_scope = 'core' if scope == 'original' and original['scope'] == 'base' else scope
            execution = ('integrated' if scope == 'integrated' else
                         SECTION_EXECUTIONS[original['section']] if scope == 'original' and original['scope'] != 'base' else 'core')
            units.append({'id': f"published:{entry['example']}:{scope}",
                          'example': entry['example'], 'scope': scope,
                          'execution_scope': execution, 'section': original['section'],
                          'input_sha256': original['input_sha256'],
                          'snapshot_sha256': original['snapshot_sha256'],
                          'source_lines': {k: original[k] for k in original if 'line' in k},
                          'source': original['markdown'],
                          'raw_ref': entry['expectations'][scope],
                          'target_ref': entry['expectations'][target_scope],
                          'raw': profile.resolve(entry, scope, originals, fixtures),
                          'target': profile.resolve(entry, target_scope, originals, fixtures),
                          'provenance': provenance(entry['expectations'][target_scope], fixtures)})
    for identity, fixture in fixtures.items():
        if identity.startswith('published-'):
            continue
        for scope in ('core', 'integrated'):
            ref = {'fixture': identity, 'scope': scope}
            units.append({'id': f'{identity}:{scope}', 'example': None,
                          'scope': scope, 'execution_scope': scope,
                          'section': fixture['spec']['section'], 'source_lines': {},
                          'input_sha256': fixture['input_sha256'],
                          'snapshot_sha256': fixture['spec']['pin'],
                          'source': fixture['source'], 'raw_ref': ref, 'target_ref': ref,
                          'raw': fixture[scope], 'target': fixture[scope],
                          'provenance': provenance(ref, fixtures)})
    profile.require(len(units) == 2117 and len({u['id'] for u in units}) == 2117,
                    'missing/duplicate complete unit inventory')
    return units


def provenance(ref, fixtures):
    if 'fixture' not in ref:
        return {'reason': 'Immutable published expectation.', 'spec': {'pin': profile.gfm_corpus.SNAPSHOT}}
    f = fixtures[ref['fixture']]
    return {'reason': f['reason'], 'spec': f['spec']}


def validate_actual(actual, source):
    profile.require(type(actual) is dict and set(actual) <= {'schema_version','status','html','catalog','positions','reason','message'} and type(actual.get('schema_version')) is int and actual.get('schema_version') == 1,
                    'invalid actual protocol version')
    profile.require({'schema_version','status','html','catalog','positions'} <= set(actual), 'missing actual outcome fields')
    for key in ('reason','message'):
        profile.require(key not in actual or type(actual[key]) is str, 'invalid actual outcome detail')
        if key in actual:
            actual[key].encode('utf-8')
    status = actual.get('status')
    profile.require(status in ('rendered', 'pending', 'error', 'interrupted'), 'unknown actual status')
    if status != 'rendered':
        profile.require(type(actual.get('catalog')) is list and type(actual.get('positions')) is list,
                        'invalid unsuccessful output fields')
        profile.require(bool(actual.get('reason')), 'missing unsuccessful outcome reason')
        if status == 'pending':
            profile.require(actual['reason'] == 'syntax-unimplemented', 'unknown pending reason')
        if status == 'interrupted':
            profile.require(actual['reason'] in ('cancelled','deadline-exceeded','limit-exceeded'), 'unknown interruption reason')
        profile.require(actual.get('html') == '' and not actual.get('catalog') and not actual.get('positions'), 'partial success output')
        return
    profile.require('reason' not in actual and 'message' not in actual, 'rendered outcome carries failure details')
    profile.require(type(actual.get('html')) is str and type(actual.get('catalog')) is list and
                    type(actual.get('positions')) is list, 'missing actual HTML/catalog/positions')
    catalog = actual['catalog']
    profile.require(0 < len(catalog) <= 20000, 'missing actual document or excessive fact inventory')
    profile.require(len(actual['positions']) <= 16385, 'excessive coordinate inventory')
    # Bound arbitrary faulty-consumer JSON and reject unpaired surrogate escapes
    # before diagnostics/report encoding can turn one bad unit into a lost run.
    stack, visited = [(actual, 0)], 0
    while stack:
        value, depth = stack.pop()
        visited += 1
        profile.require(visited <= 1000000 and depth <= 400, 'actual JSON structure limit')
        if isinstance(value, str):
            value.encode('utf-8')
        elif isinstance(value, dict):
            stack.extend((item, depth+1) for pair in value.items() for item in pair)
        elif isinstance(value, list):
            stack.extend((item, depth+1) for item in value)
    for i, f in enumerate(catalog):
        profile.fields(f, ('id', 'parent', 'kind', 'span', 'content', 'facts'), 'actual fact')
        profile.require(type(f['id']) is int and f['id'] == i and type(f['parent']) is int and
                        -1 <= f['parent'] < i, 'invalid actual identity/ancestry')
        profile.require(i == 0 or f['parent'] >= 0, 'foreign actual root')
        profile.source_span(source, f['span'])
        profile.require(f['kind'] in ACTUAL_KINDS and type(f['content']) is list and type(f['facts']) is dict,
                        'invalid actual fact fields')
        for segment in f['content']:
            profile.source_span(source, segment, f['span'][:2])
        if f['parent'] >= 0:
            p = catalog[f['parent']]['span']
            profile.require(p[0] <= f['span'][0] <= f['span'][1] <= p[1], 'fact outside actual ancestor')
    profile.require(catalog[0]['kind'] == 'document' and catalog[0]['span'] == [0, len(source.encode()), source],
                    'wrong actual document snapshot')
    profile.positions(source, actual['positions'])
    offsets = [p['offset'] for p in actual['positions']]
    boundaries, offset, encoded = [0], 0, source.encode()
    for character in source:
        offset += len(character.encode('utf-8'))
        if not (offset < len(encoded) and encoded[offset-1:offset+1] == b'\r\n'):
            boundaries.append(offset)
    profile.require(offsets == boundaries, 'missing/duplicate/unordered actual positions')


def disabled_extensions(actual, scope):
    allowed = {'table': 'tables-only', 'table-row': 'tables-only', 'table-cell': 'tables-only',
               'task-item': 'tasks-only', 'strike': 'strike-only', 'filtered-tag': 'filter-only'}
    for f in actual['catalog']:
        required = allowed.get(f['kind'])
        if f['kind'] == 'link' and f['facts'].get('origin') == 'bare-gfm':
            required = 'bare-only'
        profile.require(not required or scope in ('integrated', required),
                        f"disabled extension {f['kind']} in {scope} at fact {f['id']}")


def project(expected, actual):
    """Select ACTUAL fields using authored assertions, preserving real ancestry.

    Empty content/children are unasserted; every nonempty assertion is strict.
    Facts are selected by key, but their values always come from actual output.
    """
    catalog = actual['catalog']
    used = set()
    positions = {p['offset']: p for p in actual['positions']}

    def descendant(f, parent):
        while f['parent'] >= 0:
            if f['parent'] == parent:
                return True
            f = catalog[f['parent']]
        return False

    def nodes(wanted, parent=None):
        selected, previous = [], -1
        for e in wanted:
            matches = [f for f in catalog if f['kind'] == e['kind'] and f['span'] == e['span'] and
                       (parent is None or descendant(f, parent))]
            profile.require(len(matches) == 1, f"missing/ambiguous {e['kind']} {e['span'][:2]}")
            f = matches[0]
            profile.require(f['id'] not in used and f['id'] > previous, 'reused/unordered selected fact')
            used.add(f['id'])
            previous = f['id']
            facts = {}
            for key, value in e['facts'].items():
                if key == 'positions':
                    facts[key] = [positions.get(p['offset']) for p in value]
                else:
                    profile.require(key in f['facts'], f"fact {f['id']} missing field {key}")
                    facts[key] = f['facts'][key]
            selected.append({'kind': f['kind'], 'span': f['span'],
                             'content': f['content'] if e['content'] else [],
                             'children': nodes(e['children'], f['id']), 'facts': facts})
        return selected
    return nodes(expected)


def strict_equal(left, right):
    # Python otherwise equates True with 1, weakening typed fixture assertions.
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(strict_equal(left[k], right[k]) for k in left)
    if isinstance(left, list):
        return len(left) == len(right) and all(strict_equal(a, b) for a, b in zip(left, right))
    return left == right


def comparison(expected, actual, scope):
    status = actual.get('status')
    if status != 'rendered':
        return {'status': 'pending' if status == 'pending' else 'error',
                'reason': actual.get('reason', 'execution'), 'message': actual.get('message', '')}
    try:
        disabled_extensions(actual, scope)
        selected = project(expected['nodes'], actual)
        matched = actual['html'] == expected['html'] and strict_equal(selected, expected['nodes'])
        result = profile.consumer_result(expected, {'status': 'pass' if matched else 'fail',
                       'html': actual['html'], 'nodes': selected, 'error': None})
        diff = ''.join(difflib.unified_diff(expected['html'].splitlines(True), actual['html'].splitlines(True),
                                         fromfile='expected.html', tofile='actual.html'))
        facts_diff = ''.join(difflib.unified_diff(json.dumps(expected['nodes'], indent=2, ensure_ascii=False, sort_keys=True).splitlines(True),
                           json.dumps(selected, indent=2, ensure_ascii=False, sort_keys=True).splitlines(True),
                           fromfile='expected.facts', tofile='actual.facts'))
        return {'status': result, 'html_diff': diff, 'fact_diff': facts_diff,
                'expected_html': expected['html'], 'actual_html': actual['html'],
                'expected_bytes': repr(expected['html'].encode()), 'actual_bytes': repr(actual['html'].encode())}
    except (ValueError, KeyError, TypeError, RecursionError, OverflowError) as error:
        return {'status': 'fail', 'reason': str(error), 'expected_html': expected['html'], 'actual_html': actual['html']}


def execute(adapter, unit, timeout):
    process = None
    readers = []
    over_limit = threading.Event()
    stream_errors = []
    captured = [bytearray(), bytearray()]

    def kill_owned():
        if process is None:
            return
        try:
            if os.name == 'posix':
                os.killpg(process.pid, signal.SIGKILL)
            else:
                # Windows taskkill owns the process tree rooted at this PID.
                subprocess.run(['taskkill', '/PID', str(process.pid), '/T', '/F'],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                               check=False, timeout=5)
        except (ProcessLookupError, OSError, subprocess.SubprocessError):
            pass
        if process.poll() is None:
            process.kill()

    def collect(stream, output):
        try:
            while True:
                chunk = stream.read(8192)
                if not chunk:
                    return
                if len(chunk) > MAX_RESPONSE-len(output):
                    over_limit.set()
                    kill_owned()
                    return
                output.extend(chunk)
        except OSError as error:
            stream_errors.append(str(error))
        finally:
            stream.close()

    try:
        process = subprocess.Popen([str(adapter), '--json', '--scope', unit['execution_scope']],
                                   stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   start_new_session=os.name == 'posix')
        for stream, output in zip((process.stdout, process.stderr), captured):
            thread = threading.Thread(target=collect, args=(stream, output), daemon=True)
            thread.start()
            readers.append(thread)
        # Input is retained and finite; write on a separate thread so a faulty
        # consumer that never reads stdin cannot bypass the timeout.
        def feed():
            try:
                process.stdin.write(unit['source'].encode())
                process.stdin.close()
            except (BrokenPipeError, OSError) as error:
                stream_errors.append(str(error))
        writer = threading.Thread(target=feed, daemon=True)
        writer.start()
        process.wait(timeout=timeout)
        for thread in readers:
            thread.join(timeout=1)
        if any(thread.is_alive() for thread in readers):
            kill_owned()
            for thread in readers:
                thread.join(timeout=5)
            raise ValueError('adapter descendant retained output descriptors')
        writer.join(timeout=1)
        profile.require(not writer.is_alive(), 'adapter input writer did not finish')
        profile.require(not stream_errors, 'adapter stream failure: '+repr(stream_errors))
        profile.require(not over_limit.is_set(), 'adapter response or stderr exceeds limit')
        raw, diagnostic = map(bytes, captured)
        profile.require(process.returncode == 0, f'adapter exit {process.returncode}: {diagnostic[:2048]!r}')
        def unique_object(pairs):
            value = {}
            for key, item in pairs:
                profile.require(key not in value, 'duplicate JSON field')
                value[key] = item
            return value
        def integer(token):
            profile.require(len(token) <= 20, 'actual integer token exceeds finite range')
            value = int(token)
            profile.require(-(1<<63) <= value < 1<<63, 'actual integer outside int64')
            return value
        def noninteger(token):
            raise ValueError('actual protocol forbids floating/nonfinite numbers: '+token[:32])
        actual = json.loads(raw, object_pairs_hook=unique_object, parse_int=integer,
                            parse_float=noninteger, parse_constant=noninteger)
        validate_actual(actual, unit['source'])
        return actual
    except (OSError, subprocess.TimeoutExpired, ValueError, KeyError, TypeError, RecursionError, OverflowError) as error:
        kill_owned()
        if process is not None:
            process.wait(timeout=5)
        for thread in readers:
            thread.join(timeout=5)
        return {'schema_version': 1, 'status': 'error', 'html': '', 'catalog': [], 'positions': [],
                'reason': 'adapter-execution', 'message': str(error)[:4096]}
    finally:
        # Even a consumer that exits0 may have left detached work in its group.
        # Kill this owned group before allowing the next unit to start.
        if process is not None:
            kill_owned()


def report(units, selected, actuals):
    profile.require(len({u['id'] for u in selected}) == len(selected), 'duplicate selected unit')
    profile.require(set(actuals) <= {u['id'] for u in selected}, 'foreign or extra actual response')
    records = []
    for u in selected:
        a = actuals.get(u['id'])
        if a is None:
            raw = target = {'status': 'pending', 'reason': 'not-run'}
        else:
            raw = comparison(u['raw'], a, u['execution_scope'])
            target = comparison(u['target'], a, u['execution_scope'])
        record = {k: v for k, v in u.items() if k not in ('raw', 'target', 'source')}
        record.update({'raw_declared': raw, 'approved_target': target, 'actual': a})
        records.append(record)
    counts = {}
    for axis in ('raw_declared', 'approved_target'):
        c = Counter(r[axis]['status'] for r in records)
        counts[axis] = {k: c[k] for k in ('pass', 'fail', 'error', 'pending')}
    full = len(units) == len(selected) == len(actuals) == 2117 and set(actuals) == {u['id'] for u in units}
    raw_complete = full and counts['raw_declared']['pass'] == 2117
    target_complete = full and counts['approved_target']['pass'] == 2117
    return {'schema_version': 1, 'published_cases': 677, 'extension_cases': 28,
            'published_consumer_units': 2031, 'supplemental_consumer_units': 86,
            'full_inventory_count': len(units), 'selected_count': len(selected),
            'executed_count': len(actuals), 'filtered_out_count': len(units)-len(selected),
            'all_units_executed': full, 'counts': counts, 'units': records,
            'conformance_complete': raw_complete,
            'raw_historical_conformance_complete': raw_complete,
            'approved_profile_conformance_complete': target_complete}


def execute_selected(adapter, selected, timeout):
    actuals, retained = {}, 0
    for unit in selected:
        actual = execute(adapter, unit, timeout)
        size = len(json.dumps(actual, ensure_ascii=True).encode('ascii'))
        if size > MAX_RETAINED_ACTUAL-retained:
            actual = {'schema_version': 1, 'status': 'error', 'html': '', 'catalog': [], 'positions': [],
                      'reason': 'actual-retention-limit', 'message': '32 MiB cumulative actual response limit'}
        else:
            retained += size
        actuals[unit['id']] = actual
    return actuals


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--run', action='store_true', help='execute actual parser (default for this entry point)')
    p.add_argument('--adapter', type=Path, help='absolute or relative prebuilt mdchk_conformance-tagged adapter')
    p.add_argument('--examples', help='comma-separated published example IDs, e.g. 198,649')
    p.add_argument('--section', help='exact retained section name')
    p.add_argument('--scope', choices=profile.SCOPES, help='expectation scope, original uses section-only execution')
    p.add_argument('--fixture', help='exact supplemental fixture ID')
    p.add_argument('--timeout', type=float, default=65, help='finite per-execution timeout seconds (0 < value <= 120)')
    p.add_argument('--require-complete', action='store_true', help='require full immutable raw historical equality')
    p.add_argument('--require-approved-profile', action='store_true', help='require full approved modern-target conformance')
    p.add_argument('--json', action='store_true', help='complete deterministic versioned actual report')
    args = p.parse_args(argv)
    try:
        profile.require(0 < args.timeout <= 120, 'timeout must be finite and at most 120 seconds')
        # Omission is unrestricted; a supplied blank value is an invalid request,
        # never permission to drop a filter and broaden the requested intersection.
        for flag in ('examples', 'section', 'fixture'):
            value = getattr(args, flag)
            profile.require(value is None or bool(value.strip()),
                            '--'+flag+' requires a nonempty value')
        originals, fixtures = profile.load()
        data = json.loads((profile.PROFILE/'ledger.json').read_bytes())
        profile.verify_profile_artifact('ledger.json')
        units = inventory(data, originals, fixtures)
        examples = None
        if args.examples:
            examples = {int(x) for x in args.examples.split(',')}
            profile.require(examples and examples <= set(range(1, 678)), 'unknown example selection')
        if args.section:
            profile.require(args.section in {u['section'] for u in units}, 'unknown section')
        if args.fixture:
            profile.require(args.fixture in fixtures and not args.fixture.startswith('published-'), 'unknown supplemental fixture')
        selected = [u for u in units if (examples is None or u['example'] in examples) and
                    (not args.scope or u['scope'] == args.scope) and
                    (not args.section or u['section'] == args.section) and
                    (not args.fixture or u['id'].startswith(args.fixture+':'))]
        profile.require(bool(selected), 'empty selection')
        with tempfile.TemporaryDirectory(prefix='mdchk-gfm-') as temp:
            adapter = args.adapter.resolve() if args.adapter else Path(temp)/'specadapter'
            if not args.adapter:
                subprocess.run(['go', 'build', '-tags', 'mdchk_conformance', '-o', str(adapter),
                                './internal/conformance/cmd/specadapter'], cwd=profile.ROOT,
                               check=True, timeout=120)
            actuals = execute_selected(adapter, selected, args.timeout)
        result = report(units, selected, actuals)
        if args.json:
            print(json.dumps(result, ensure_ascii=False, sort_keys=True))
        else:
            print(f"GFM actual execution: {result['executed_count']}/{result['full_inventory_count']} units; "
                  f"filtered={result['filtered_out_count']}; raw_declared={result['counts']['raw_declared']}; "
                  f"approved_target={result['counts']['approved_target']}")
            for r in result['units']:
                if r['raw_declared']['status'] != 'pass' or r['approved_target']['status'] != 'pass':
                    print(json.dumps({k: v for k, v in r.items() if k != 'actual'}, ensure_ascii=False, sort_keys=True))
            print(f"raw_historical_conformance_complete={result['raw_historical_conformance_complete']}; "
                  f"approved_profile_conformance_complete={result['approved_profile_conformance_complete']}")
        if args.require_complete and not result['conformance_complete']:
            return 1
        if args.require_approved_profile and not result['approved_profile_conformance_complete']:
            return 1
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'GFM actual conformance: {error}', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
