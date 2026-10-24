import copy
import hashlib
import json
from pathlib import Path
from jsonschema import Draft202012Validator

ROOT=Path(__file__).parent
schema=json.loads((ROOT/'result.schema.json').read_text())
Draft202012Validator.check_schema(schema)
validator=Draft202012Validator(schema)
fixtures=json.loads((ROOT/'fixtures.json').read_text())

def require(ok,message):
    if not ok:raise ValueError(message)
def semantic(x):
    if 'operation' not in x:return
    records=[r for key in ['diagnostics','coverage','changes','deferred_fixes','errors','notes','interruptions'] for r in x[key]]
    records += [s['diagnostic'] for s in x['suppressed_diagnostics']]
    ids=[r['id'] for r in records]
    require(len(ids)==len(set(ids)),'duplicate record id')
    names=[f['path'] for f in x['files']]
    require(len(names)==len(set(names)),'duplicate target path')
    targets={f['path']:f for f in x['files']}
    for key in ['checked_files']:
        require(len(x[key])==len(set(x[key])),'duplicate checked path')
        require(all(p in targets for p in x[key]),'checked target missing outcome')
    for f in x['files']:
        for field,collection in [('error_ids','errors'),('coverage_ids','coverage'),('change_ids','changes')]:
            valid={r['id'] for r in x[collection]}
            require(all(i in valid for i in f[field]),'unresolved file reference')
        if f['write_status']=='written':
            require(x['operation']=='fix' and x['mode']!='standalone' and not x['dry_run'],'written file in read-only mode')
            require(any(c['path']==f['path'] and c['disposition']=='written' for c in x['changes']),'written file lacks confirmed change')
    for suppressed in x['suppressed_diagnostics']:
        diagnostic=suppressed['diagnostic'];path=diagnostic['path']
        require(path in targets,'suppressed diagnostic lacks target')
        expected=targets[path].get('input_snapshot')
        if diagnostic['snapshot']['view'] in ('candidate','saved') and x['operation']=='fix':expected=targets[path].get('result_snapshot')
        require(diagnostic['snapshot']==expected,'suppressed snapshot mismatch')
        if x['operation']=='lint':require('directive_id' not in suppressed['suppressed_by'],'lint lacks directive records for attribution ids')
    for key in ['coverage','deferred_fixes']:
        for item in x[key]:require(all(i in ids for i in item.get('cause_ids',[])),'unresolved cause reference')
    docs={d['path']:d for d in x['documents']}
    require(len(docs)==len(x['documents']),'duplicate returned document')
    for p,d in docs.items():
        require(p in targets and targets[p]['return_status']=='returned','content without returned outcome')
        data=d['content'].encode('utf-8')
        require(len(data)==d['snapshot']['size_bytes'] and hashlib.sha256(data).hexdigest()==d['snapshot']['sha256'],'content snapshot mismatch')
        require(targets[p].get('result_snapshot')==d['snapshot'],'file/document snapshot mismatch')
        require(d['disposition']==('proposed' if x['dry_run'] else 'returned'),'wrong returned-content disposition')
    for p,f in targets.items():
        require((f['return_status']=='returned')==(p in docs),'returned status/content mismatch')
        if f['return_status']=='withheld':require(any(o.get('path')==p and o['section']=='/documents' for o in x['omissions']),'withheld content lacks omission')
    for c in x['changes']:
        require(c['path'] in targets,'change lacks target outcome')
        if c['disposition']=='written':require(targets[c['path']]['write_status']=='written','confirmed write lacks written outcome')
        if x.get('dry_run'):require(c['disposition']=='proposed','dry-run non-proposed change')
        for h in c.get('diff',{}).get('hunks',[]):
            require(h['old_lines']==sum(l['kind']!='add' for l in h['lines']),'old diff line count mismatch')
            require(h['new_lines']==sum(l['kind']!='remove' for l in h['lines']),'new diff line count mismatch')
            require(all('\n' not in l['text'] and '\r' not in l['text'] for l in h['lines']),'diff text contains terminator')
    for item in records:
        if 'range' in item:
            a=item['range']['start'];b=item['range']['end']
            require((a['line'],a['column'])<=(b['line'],b['column']),'reversed source range')
        if 'byte_range' in item:
            require(0<=item['byte_range']['start']<=item['byte_range']['end']<=item['snapshot']['size_bytes'],'invalid byte range')
    if x['status']=='inspected':require(not any(e['scope']=='operation' for e in x['errors']),'operational error reported as completed inspection')
    if x['mode']=='standalone':require(all(f['write_status']=='not-requested' for f in x['files']),'standalone write outcome')
    if x['operation']=='config validate' and 'config_validation' in x:
        s=x['config_validation'];require(s['valid_definitions']+s['invalid_definitions']==s['definitions_checked'],'configuration count mismatch')
    if 'inspection' in x:
        inspection_semantic(x)
        r=x['inspection']['rule_counts']
        if r['availability']=='available':require(r['data']['active']+r['data']['inactive']==r['data']['known'],'catalog count mismatch')

def inspection_semantic(x):
    p=x['inspection'];ids={r['id'] for k in ['errors','interruptions','coverage'] for r in x[k]}
    for name,section in p.items():
        if isinstance(section,dict) and 'availability' in section:require(all(c in ids for c in section.get('cause_ids',[])),'inspection section has unresolved cause')
    size=p['file']['size_bytes'];snapshot={'view':'original','sha256':p['file']['sha256'],'size_bytes':size}
    def section(name):return p.get(name,{}).get('data')
    def bounded(r):require(0<=r['start']<=r['end']<=size,'inspection byte range outside snapshot')
    directives=section('directives') or []
    by_id={d['id']:d for d in directives}
    require(len(by_id)==len(directives),'duplicate directive id')
    for d in directives:
        bounded(d['byte_range']);require(d['byte_range']['start']<=d['closing_offset']<=d['byte_range']['end'],'directive closing offset outside envelope')
        for token in d.get('named_tokens',[]):bounded(token['byte_range']);require(d['byte_range']['start']<=token['byte_range']['start']<=token['byte_range']['end']<=d['byte_range']['end'],'directive token outside envelope')
    ast=section('ast')
    if ast:
        require(ast['snapshot']==snapshot,'AST snapshot identity mismatch')
        if section('flavor') is not None:require(ast['profile']==section('flavor'),'AST profile differs from selected flavor')
        node_ids=set()
        def walk(n,parent=None):
            require(n['id'] not in node_ids,'duplicate AST node id');node_ids.add(n['id']);bounded(n['byte_range'])
            if parent:require(parent['start']<=n['byte_range']['start']<=n['byte_range']['end']<=parent['end'],'AST child outside parent')
            facts=n['properties'];envelope=n['byte_range']
            def owned(r):
                bounded(r);require(envelope['start']<=r['start']<=r['end']<=envelope['end'],'GFM fact outside owner')
            def value_facts(v):
                for segment in v['segments']:
                    if 'byte_range' in segment:owned(segment['byte_range'])
            if 'alignments' in facts:
                require(n['kind']=='table' and facts.get('width',0)>0 and len(facts['alignments'])==facts['width'],'invalid GFM table width/alignment')
            if 'raw_cells' in facts:
                require(n['kind']=='table-row' and facts.get('role') in ('header','delimiter','body') and facts.get('padding',-1)>=0,'invalid GFM row facts')
                require(facts['raw_cells']>=len(n['children']),'GFM row lost original cell count')
            for key in ('separators','escaped_pipes'):
                if key in facts:
                    require(n['kind']==('table-row' if key=='separators' else 'table-cell'),'GFM facts on wrong kind')
                    previous_end=-1
                    for span in facts[key]:
                        owned(span);require(span['start']>=previous_end and span['end']-span['start']==(1 if key=='separators' else 2),'invalid GFM source tokens');previous_end=span['end']
            for key in ('leading_pipe','trailing_pipe'):
                if key in facts:
                    owned(facts[key]);require(facts[key] in facts.get('separators',[]),'outer pipe lacks original separator')
            if 'task' in facts:
                require(n['kind'] in ('list-item','paragraph'),'task on wrong kind');task=facts['task']
                for key in ('paragraph','marker','opening','check','closing','separator'):owned(task[key])
                require(task['marker']['start']==task['opening']['start'] and task['marker']['end']==task['closing']['end'],'invalid task marker envelope')
                require(task['checked']==(task['check_spelling'] in ('x','X')),'task state/spelling mismatch')
                for span in task['segments']:
                    owned(span);require(task['marker']['start']<=span['start']<=span['end']<=task['marker']['end'],'task contribution outside marker')
            if 'filtered_tags' in facts:
                require(n['kind'] in ('html-block','inline-html'),'filtered tag on wrong owner');last=-1
                for tag in facts['filtered_tags']:
                    owned(tag['affected']);require(tag['affected']['end']==tag['affected']['start']+1 and tag['affected']['start']>last,'invalid filtered angle');last=tag['affected']['start']
                    value_facts(tag['prefix'])
                    if 'tag' in tag:value_facts(tag['tag'])
            previous=-1
            for child in n['children']:
                require(child['byte_range']['start']>=previous,'AST children out of source order');previous=child['byte_range']['start'];walk(child,n['byte_range'])
        walk(ast['root']);require(ast['root']['kind']=='document' and ast['root']['byte_range']=={'start':0,'end':size},'AST lacks full document root')
        for reference in ast.get('references',[]):bounded(reference['byte_range'])
        header=ast.get('frontmatter')
        if header:
            envelope=header['byte_range'];bounded(envelope)
            require(envelope['start']==0 and envelope['end']==header['body_offset'],'frontmatter body offset differs from envelope')
            require(header['range']['start']=={'line':1,'column':1},'frontmatter does not start at document origin')
            for key in ('opener','content','closer'):
                if key in header:
                    region=header[key]['byte_range'];bounded(region)
                    require(envelope['start']<=region['start']<=region['end']<=envelope['end'],'frontmatter component outside envelope')
            opener=header['opener']['byte_range'];content=header['content']['byte_range']
            require(opener['start'] in (0,3) and opener['end']-opener['start']>=3,'invalid frontmatter opener')
            require(0<=content['start']-opener['end']<=2,'invalid frontmatter opening ending')
            def point(value):return (value['line'],value['column'])
            def after_ending(value, width):return value if width==0 else {'line':value['line']+1,'column':1}
            require(header['opener']['range']['start']=={'line':1,'column':1+(opener['start']==3)},'invalid frontmatter opener coordinate')
            require(header['opener']['range']['end']=={'line':1,'column':header['opener']['range']['start']['column']+opener['end']-opener['start']},'frontmatter opener coordinate width mismatch')
            require(header['content']['range']['start']==after_ending(header['opener']['range']['end'],content['start']-opener['end']),'frontmatter payload start coordinate mismatch')
            for key in ('opener','content','closer'):
                if key in header:
                    location=header[key]['range']
                    require(point(header['range']['start'])<=point(location['start'])<=point(location['end'])<=point(header['range']['end']),'frontmatter coordinates outside envelope')

            if 'closer' in header:
                closer=header['closer']['byte_range']
                require(content['end']==closer['start'] and closer['end']-closer['start']>=3 and 0<=envelope['end']-closer['end']<=2,'invalid frontmatter closer boundary')
                require(header['content']['range']['end']==header['closer']['range']['start'],'frontmatter payload end coordinate mismatch')
                require(header['closer']['range']['start']['column']==1 and header['closer']['range']['end']=={'line':header['closer']['range']['start']['line'],'column':1+closer['end']-closer['start']},'frontmatter closer coordinate width mismatch')
                require(header['range']['end']==after_ending(header['closer']['range']['end'],envelope['end']-closer['end']),'frontmatter envelope ending coordinate mismatch')

            else:
                require(content['end']==envelope['end']==size,'unterminated frontmatter does not reach EOF')
                require(header['content']['range']['end']==header['range']['end'],'unterminated frontmatter coordinate mismatch')
                require(p['ast']['availability']=='partial' and not x['complete'],'unterminated frontmatter claims complete parse')
                require(any(n.get('code')=='unterminated-frontmatter' and n.get('byte_range')==envelope for n in (section('parsing_notes') or [])),'unterminated frontmatter lacks parsing note')
            require(all(n['byte_range']['start']>=header['body_offset'] for n in ast['root']['children']),'header leaked into body AST')
            require(all(n['byte_range']['start']>=header['body_offset'] for n in ast.get('references',[])),'header leaked into body references')

    selected_region=p['file'].get('selected_region')
    if 'selected_lines' in p['file']:
        selected=p['file']['selected_lines']
        require(1<=selected['start']<=selected['end']<=p['file'].get('physical_line_count',selected['end']),'invalid selected physical lines')
    def intersects(v):
        q=selected_region['byte_range']
        if v['start']==v['end']:return q['start']<=v['start'] and (v['start']<q['end'] or selected_region['includes_eof'] and v['start']==q['end'])
        return v['start']<q['end'] and q['start']<v['end']
    if selected_region:
        bounded(selected_region['byte_range'])
        require(not selected_region['includes_eof'] or selected_region['byte_range']['end']==size,'selection claims nonterminal EOF')
    if ast and ast.get('frontmatter'):
        header=ast['frontmatter']
        expected_context=bool(selected_region and not intersects(header['byte_range']))
        require(header['context_only']==expected_context,'frontmatter selection context mismatch')
        expected_extends=bool(selected_region and (header['byte_range']['start']<selected_region['byte_range']['start'] or header['byte_range']['end']>selected_region['byte_range']['end']))
        require(header.get('extends_region',False)==expected_extends,'frontmatter selected extent mismatch')
    rules=section('rules') or []
    require(len({r['rule'] for r in rules})==len(rules),'duplicate inspected rule')
    for r in rules:
        for interval in r['intervals']:
            bounded(interval['byte_range'])
            if interval['origin']=='directive':
                require(interval.get('directive_id') in by_id,'unresolved interval directive')
                owner=by_id[interval['directive_id']]
                require(interval['byte_range']['start']==owner['closing_offset'],'directive interval starts before or after owner')
                require(any(e['rule']==r['rule'] and e['effect'] in ['enable','disable','no-op'] and e['permission']=='allowed' for e in owner['effects']),'blocked directive owns interval')
        if 'source_coverage' in r:
            for mode,status in [('active','active_evaluation'),('observational','observational_evaluation')]:
                coverage=r['source_coverage'][mode]
                require(len(coverage['ranges'])==len(coverage['byte_ranges']),'coverage coordinate lengths differ')
                for region in coverage['byte_ranges']:bounded(region)
                if coverage['availability']=='complete':require(r[status]=='complete' and coverage['byte_ranges']==[{'start':0,'end':size}],'unproven complete rule source coverage')
                else:require(not coverage['byte_ranges'] and not coverage['ranges'],'unknown coverage claims evaluated regions')
    if selected_region:
        counts=section('rule_counts')
        resolved=all(r['effective_state']!='unresolved' for r in rules) and section('rules') is not None
        if counts:
            require(resolved,'counts claim unresolved effective state')
            require(counts['known']==len(rules),'counts omit known catalog records')
        active=0
        for r in rules:
            if r['effective_state']=='unresolved':require('selected_enabled' not in r,'selected enablement claims unresolved state');continue
            enabled=any(i['enabled'] and intersects(i['byte_range']) for i in r['intervals'])
            require(r.get('selected_enabled')==enabled,'selected rule enablement mismatch')
            active+=enabled
        if counts:require(counts['active']==active and counts['inactive']==len(rules)-active,'selected counts differ from intervals')
        for d in x['diagnostics']+[h['diagnostic'] for h in x['suppressed_diagnostics']]:
            require(intersects(d['byte_range']),'finding outside selected region')
    for candidate in section('raw_candidates') or []:
        bounded(candidate['byte_range'])
        if 'directive_id' in candidate:require(candidate['directive_id'] in by_id,'raw candidate has dangling directive id')
    for hidden in x['suppressed_diagnostics']:
        cause=hidden['suppressed_by'];d=hidden['diagnostic']
        if cause['kind']=='directive':
            require(cause.get('directive_id') in by_id,'suppressed diagnostic has dangling directive id')
            require(any(r['rule']==d['rule'] and any(not interval['enabled'] and interval.get('directive_id')==cause['directive_id'] and interval['byte_range']['start']<=d['byte_range']['start'] and (d['byte_range']['start']<interval['byte_range']['end'] or d['byte_range']['start']==interval['byte_range']['end']==size) for interval in r['intervals']) for r in rules),'suppressed diagnostic owner does not control anchor')

def exit_code(x):
    errors=[e for e in x['errors'] if e['scope']=='operation']
    cats={e['category'] for e in errors}
    if 'internal' in cats:return 14
    if 'io' in cats:return 13
    if 'usage' in cats:return 11
    if 'configuration' in cats:return 12
    if x['operation']=='inspect':return 15 if x['interruptions'] or x['omissions'] else 0
    if not x['complete']:return 15
    if x['operation']=='config validate':return 0
    return 10 if x['diagnostics'] else 0

for entry in fixtures:
    data=json.loads((ROOT/entry['file']).read_text())
    v=validator if entry['kind']=='result' else Draft202012Validator({'$schema':schema['$schema'],'$ref':'#/$defs/'+entry['definition'],'$defs':schema['$defs']})
    v.validate(data)
    if entry['kind']=='result':
        semantic(data)
        if 'expected_cli_exit' in entry:require(exit_code(data)==entry['expected_cli_exit'],entry['file']+' exit mismatch')

negative=[]
def reject(name,data):
    try:validator.validate(data);semantic(data)
    except Exception:negative.append(name);return
    raise AssertionError('invalid fixture accepted: '+name)
a=json.loads((ROOT/'clean-lint.json').read_text())
b=copy.deepcopy(a);b['errors']=None;reject('null-collection',b)
b=copy.deepcopy(a);b['unknown']=True;reject('undeclared-producer-field',b)
b=copy.deepcopy(a);b['interruptions']=[{'id':'stop-1','reason':'cancelled','phase':'analysis','message':'Stopped'}];reject('clean-with-interruption',b)
b=json.loads((ROOT/'fix-preview.json').read_text());b['changes'][0]['disposition']='written';reject('preview-claims-write',b)
b=json.loads((ROOT/'standalone-full-contents.json').read_text());b['documents'][0]['content']='wrong';reject('content-hash-mismatch',b)
b=json.loads((ROOT/'withheld-whole-content.json').read_text());b['omissions']=[];reject('unexplained-content-withholding',b)
b=json.loads((ROOT/'partial-project-writes.json').read_text());b['files'][0]['change_ids']=['not-present'];reject('dangling-change-reference',b)
b=json.loads((ROOT/'fix-preview.json').read_text());b['changes'][0]['diff']['hunks'][0]['new_lines']=2;reject('invalid-diff-line-count',b)
b=json.loads((ROOT/'inspection-invalid-utf8.json').read_text());b['inspection']['ast']['data']={};reject('unavailable-section-contains-data',b)
b=json.loads((ROOT/'invalid-utf8-enabled.json').read_text());b['errors'][0]['range']['start']['column']=0;reject('zero-public-column',b)
b=json.loads((ROOT/'violations-lint.json').read_text());b['suppressed_diagnostics']=[{'diagnostic':copy.deepcopy(b['diagnostics'][0]),'suppressed_by':{'kind':'directive'}}];reject('duplicate-suppressed-id',b)
b['suppressed_diagnostics'][0]['diagnostic']['id']='hidden-1';b['suppressed_diagnostics'][0]['diagnostic']['byte_range']={'start':0,'end':100000000};reject('suppressed-source-range',b)
result={'schema_valid':True,'positive_fixtures':len(fixtures),'negative_fixtures_rejected':len(negative),'negative_cases':negative,'validator':'jsonschema 4.25.1, JSON Schema 2020-12','scope':'Validates accepted serialization, representative cross-record invariants and expected fixture exits; does not test an mdchk implementation.'}
print(json.dumps(result,indent=2))
