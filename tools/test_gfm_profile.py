"""Independent literal witnesses, semantic mutation controls and offline accounting.

These are fixture-validity tests. Fake consumer projections exercise comparison
failure paths; they are explicitly not production parser observations.
"""
import copy
import hashlib
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

import gfm_profile as p


class ProfileTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.originals, cls.fixtures = p.load()
        cls.data = json.loads((p.PROFILE/'expectations.json').read_bytes())
        cls.ledger = json.loads((p.PROFILE/'ledger.json').read_bytes())

    def fixture(self, identity):
        return copy.deepcopy(self.fixtures[identity])

    def reject_fixture(self, fixture, message):
        with self.assertRaisesRegex(ValueError, message):
            p.validate_fixtures(dict(self.data, fixtures=[fixture]))

    def test_complete_scopes_and_immutable_originals(self):
        p.validate_ledger(self.ledger, self.originals, self.fixtures)
        self.assertEqual(len(self.ledger['cases']), 677)
        self.assertEqual(sum(c['scope'] != 'base' for c in self.originals), 28)
        for n in (176, 188):
            self.assertEqual(p.resolve(self.ledger['cases'][n-1], 'original', self.originals, self.fixtures)['html'], '')
        for n in p.gfm_corpus.EXTENSIONS:
            self.assertIn('published-'+str(n), self.fixtures)
            for scope in ('core', 'integrated'):
                self.assertIn('html', p.resolve(self.ledger['cases'][n-1], scope, self.originals, self.fixtures))
        self.assertNotEqual(self.fixtures['published-620']['core']['html'], self.fixtures['published-620']['integrated']['html'])
        self.assertEqual(self.originals[648]['html'], '<p>foo &lt;!-- not a comment -- two hyphens --&gt;</p>\n')

    def test_literal_modern_and_interaction_outputs(self):
        literals = {
            649: '<p>foo <!-- not a comment -- two hyphens --></p>\n',
            650: '<p>foo <!--> foo --&gt;</p>\n<p>foo <!-- foo---></p>\n',
            611: '<p>&lt;<a href="http://foo.bar/baz">http://foo.bar/baz</a> bim&gt;</p>\n',
            615: '<p>&lt;foo\\<a href="mailto:+@bar.example.com">+@bar.example.com</a>&gt;</p>\n',
            617: '<p>&lt; <a href="http://foo.bar">http://foo.bar</a> &gt;</p>\n',
            620: '<p><a href="http://example.com">http://example.com</a></p>\n',
            621: '<p><a href="mailto:foo@bar.example.com">foo@bar.example.com</a></p>\n',
            145: '&lt;style>p{color:red;}&lt;/style>\n<p><em>foo</em></p>\n',
            147: '&lt;script>\nfoo\n&lt;/script>1. *bar*\n',
            142: '&lt;style\n  type="text/css">\n\nfoo\n',
            141: '&lt;style\n  type="text/css">\nh1 {color:red;}\n\np {color:blue;}\n&lt;/style>\n<p>okay</p>\n',
            140: '&lt;script type="text/javascript">\n// JavaScript example\n\ndocument.getElementById("demo").innerHTML = "Hello JavaScript!";\n&lt;/script>\n<p>okay</p>\n',
        }
        for identity, html in literals.items():
            with self.subTest(identity=identity):
                self.assertEqual(self.fixtures['published-'+str(identity)]['integrated']['html'], html)
        for n, visible in ((611, [1,19,'http://foo.bar/baz']), (615, [5,22,'+@bar.example.com']),
                           (617, [2,16,'http://foo.bar'])):
            self.assertEqual(self.fixtures['published-'+str(n)]['integrated']['nodes'][0]['facts']['visible'], visible)
        self.assertEqual(self.fixtures['published-620']['core']['html'], '<p>http://example.com</p>\n')
        self.assertEqual(self.fixtures['published-615']['core']['html'], '<p>&lt;foo+@bar.example.com&gt;</p>\n')

    def test_literal_nonadoptions_and_boundaries(self):
        strong = {
            398: '<p><strong>foo, <strong>bar</strong>, baz</strong></p>\n',
            426: '<p>foo<strong><strong><strong>bar</strong></strong></strong>***baz</p>\n',
            434: '<p><strong>foo <strong>bar</strong> baz</strong></p>\n',
            435: '<p><strong><strong>foo</strong> bar</strong></p>\n',
            436: '<p><strong>foo <strong>bar</strong></strong></p>\n',
            473: '<p><strong><strong>foo</strong></strong></p>\n',
            474: '<p><strong><strong>foo</strong></strong></p>\n',
            475: '<p><strong><strong><strong>foo</strong></strong></strong></p>\n',
            477: '<p><em><strong><strong>foo</strong></strong></em></p>\n',
            324: '<p>&amp;nbsp &amp;x; &amp;#; &amp;#x;\n&amp;#87654321;\n&amp;#abcdef0;\n&amp;ThisIsNotDefined; &amp;hi?;</p>\n',
            634: '<p><a href="xmpp:foo@bar.baz/txt">xmpp:foo@bar.baz/txt</a></p>\n<p><a href="xmpp:foo@bar.baz/txt@bin">xmpp:foo@bar.baz/txt@bin</a></p>\n<p><a href="xmpp:foo@bar.baz/txt@bin.com">xmpp:foo@bar.baz/txt@bin.com</a></p>\n',
            635: '<p><a href="xmpp:foo@bar.baz/txt">xmpp:foo@bar.baz/txt</a>/bin</p>\n',
        }
        for n, html in strong.items():
            self.assertEqual(self.fixtures['published-'+str(n)]['integrated']['html'], html)
        for n, depth in ((398,2),(426,3),(434,2),(435,2),(436,2),(473,2),(474,2),(475,3),(477,2)):
            nodes = self.fixtures['published-'+str(n)]['integrated']['nodes']
            count = 0
            while nodes:
                count += 1
                self.assertEqual(nodes[0]['kind'], 'strong')
                self.assertEqual(nodes[0]['facts']['opening'][2], nodes[0]['facts']['closing'][2])
                nodes = nodes[0]['children']
            self.assertEqual(count, depth)
        self.assertEqual(self.fixtures['published-634']['integrated']['nodes'][1]['facts']['visible'][2], 'xmpp:foo@bar.baz/txt@bin')
        self.assertEqual(self.fixtures['published-635']['integrated']['nodes'][0]['span'], [0,20,'xmpp:foo@bar.baz/txt'])

    def test_literal_extension_source_facts(self):
        rows = self.fixtures['published-204']['integrated']['nodes'][0]['facts']['rows']
        self.assertEqual([r['source_count'] for r in rows], [2,2,1,3])
        self.assertEqual(rows[2]['padding'], [{'span':None,'value':'','padding':True}])
        self.assertEqual(rows[3]['cells'][2]['span'], [49,54,' boo '])
        self.assertEqual(rows[3]['cells'][2]['value'], 'boo')
        empty=self.fixtures['table-code-pipe-crlf']['integrated']['nodes'][0]['facts']['rows'][-1]['cells'][0]
        self.assertEqual(empty, {'span':[40,41,' '],'value':'','padding':False})
        task = self.fixtures['task-uppercase-crlf']['integrated']['nodes'][0]['children'][0]['children'][0]['facts']
        self.assertEqual(task['marker'], [4,7,'[X]'])
        self.assertEqual(task['check'], [5,6,'X'])
        self.assertEqual(task['separator'], [7,8,' '])
        paragraph=self.fixtures['task-uppercase-crlf']['integrated']['nodes'][0]['children'][0]['children'][0]['children'][0]
        self.assertEqual(paragraph['facts']['ending'],[10,12,'\r\n'])
        self.assertEqual(paragraph['content'],[[8,10,'é']])
        self.assertEqual([n['facts']['arity'] for n in self.fixtures['published-491']['integrated']['nodes']], [2,1])
        self.assertEqual(self.fixtures['published-493']['integrated']['nodes'][0]['kind'], 'text')
        self.assertEqual(self.fixtures['published-203']['integrated']['nodes'][0]['kind'], 'paragraph')
        self.assertEqual(self.fixtures['published-622']['integrated']['nodes'][0]['facts']['parts'][0], {'source':None,'value':'http://','transformation':'synthetic'})
        names = set()
        for f in self.fixtures.values():
            for n in f['integrated']['nodes']:
                if n['kind'] == 'filtered-tag':
                    names.add(n['facts']['name'])
        self.assertEqual(names, p.NAMES)
        self.assertEqual(self.fixtures['filter-nearmisses']['core']['html'], self.fixtures['filter-nearmisses']['integrated']['html'])

    def test_literal_modern_core_witnesses(self):
        expectations = {
            'textarea':'<textarea>\n*raw*\n\n[raw]: /x\n</textarea>\n',
            'search':'<p>before</p>\n<search>\n*raw*\n',
            'source':'<p>before\n<source>\n<em>live</em></p>\n',
            'lower-declaration':'<!doctype html>\n<p><em>live</em></p>\n',
            'symbol-flanking':'<p>*£*bravo.</p>\n',
            'reference-fold':'<p><a href="/target">ẞ</a></p>\n',
            'entity-one-pass':'<p>&amp;copy; � \u0080</p>\n',
            'link-group':'<p><a href="foo(and(bar))">a</a></p>\n',
            'vt-not-blank':'<p>a\n\x0b\nb</p>\n',
            'ff-not-blank':'<p>a\n\x0c\nb</p>\n',
            'control-destination':'<p>[a](x\x0by)</p>\n',
            'tab-space':'<p><a href="/x" title="t">a</a></p>\n',
            'html-ff-boundary':'<p>&lt;div\x0cclass=x&gt;\n<em>live</em></p>\n',
        }
        for identity,html in expectations.items():
            self.assertEqual(self.fixtures['modern-'+identity]['core']['html'],html)
        self.assertEqual(self.fixtures['modern-search']['core']['nodes'][0]['span'],[7,22,'<search>\n*raw*\n'])
        self.assertEqual(self.fixtures['modern-lower-declaration']['core']['nodes'][0]['span'],[0,16,'<!doctype html>\n'])
        self.assertEqual(self.fixtures['frontmatter']['core']['html'],'<hr />\n<h2>name: value</h2>\n')
        self.assertEqual(self.fixtures['frontmatter']['core'],self.fixtures['frontmatter']['integrated'])
        self.assertNotEqual(self.fixtures['combined-all-five']['core']['html'],self.fixtures['combined-all-five']['integrated']['html'])
        for prefix in p.NAMES:
            f=self.fixtures['filter-'+prefix]
            tags=[n for n in f['integrated']['nodes'] if n['kind']=='filtered-tag']
            self.assertEqual([n['facts']['closing'] for n in tags],[False,True])
            self.assertEqual([n['facts']['name'] for n in tags],[prefix,prefix])
            self.assertIn('*raw*',f['integrated']['html'])
        for identity in ('code-protection','block-code-protection','comment-protection'):
            self.assertFalse(any(n['kind']=='filtered-tag' for n in self.fixtures[identity]['integrated']['nodes']))

    def test_repaired_raw_html_owners_and_old_wrong_controls(self):
        expected = {'published-140':(0,130),'published-141':(0,68),'published-142':(0,31),
                    'published-145':(0,29),'published-147':(0,31),'combined-all-five':(35,42)}
        for identity,(start,end) in expected.items():
            fixture=self.fixtures[identity]
            tags=[n for n in fixture['integrated']['nodes'] if n['kind']=='filtered-tag']
            self.assertTrue(tags)
            for tag in tags:
                self.assertEqual(tag['facts']['protected'][:2],[start,end])
            wrong=self.fixture(identity)
            tag=next(n for n in wrong['integrated']['nodes'] if n['kind']=='filtered-tag')
            if end!=len(wrong['source'].encode()):
                tag['facts']['protected']=[0,len(wrong['source'].encode()),wrong['source']]
                self.reject_fixture(wrong,'must equal one asserted raw HTML owner')
        fixture=self.fixtures['published-657']
        tags=[n for n in fixture['integrated']['nodes'] if n['kind']=='filtered-tag']
        self.assertEqual([n['facts']['protected'][:2] for n in tags],[[9,16],[17,24],[31,108],[31,108]])
        for index in range(4):
            wrong=self.fixture('published-657');tags=[n for n in wrong['integrated']['nodes'] if n['kind']=='filtered-tag']
            tags[index]['facts']['protected']=[0,108,wrong['source']]
            self.reject_fixture(wrong,'must equal one asserted raw HTML owner')
        # Even moving BOTH the raw owner and its filter facts over prose fails.
        for identity in ('published-140','published-141','published-145'):
            wrong=self.fixture(identity);whole=[0,len(wrong['source'].encode()),wrong['source']]
            owner=wrong['integrated']['nodes'][0];owner['span']=whole;owner['content']=[whole]
            for tag in wrong['integrated']['nodes'][1:]:tag['facts']['protected']=whole
            self.reject_fixture(wrong,'must stop at its closing-tag line')
        wrong=self.fixture('combined-all-five')
        wrong['integrated']['nodes']=[n for n in wrong['integrated']['nodes'] if n['kind']!='inline-html']
        self.reject_fixture(wrong,'must equal one asserted raw HTML owner')

    def test_repaired_entity_scalar_and_old_browser_remapping(self):
        for scope in ('core','integrated'):
            fixture=self.fixtures['modern-entity-one-pass'][scope]
            self.assertEqual(fixture['html'],'<p>&amp;copy; � \u0080</p>\n')
            value=fixture['nodes'][0]['facts']['value']
            self.assertEqual(value,'&copy; � \u0080')
            self.assertEqual(ord(value[-1]),0x80)
            self.assertNotEqual(ord(value[-1]),0x20ac)
            self.assertEqual(fixture['nodes'][0]['facts']['parts'][-1]['source'],[16,22,'&#x80;'])
            wrong=self.fixture('modern-entity-one-pass')
            wrong[scope]['html']='<p>&amp;copy; � €</p>\n'
            wrong[scope]['nodes'][0]['facts']['value']='&copy; � €'
            wrong[scope]['nodes'][0]['facts']['parts'][-1]['value']='€'
            self.reject_fixture(wrong,'Unicode scalar without browser remapping')

    def test_repaired_nested_task_owners_and_old_wrong_controls(self):
        fixture=self.fixtures['published-280'];root=fixture['integrated']['nodes'][0]
        self.assertEqual(root['kind'],'list')
        self.assertEqual(root['span'][:2],[0,44])
        outer,bim=root['children'];nested=outer['children'][0]
        self.assertEqual(outer['span'][:2],[0,34]);self.assertEqual(outer['facts']['item'][:2],[0,34])
        self.assertEqual(outer['facts']['paragraph'],[2,10,'[x] foo\n'])
        self.assertEqual(nested['kind'],'list');self.assertEqual(nested['span'][:2],[12,34])
        self.assertEqual([n['span'][:2] for n in nested['children']],[[12,22],[24,34]])
        self.assertEqual([n['facts']['paragraph'][:2] for n in nested['children']],[[14,22],[26,34]])
        self.assertEqual(outer['facts']['continuation_prefixes'],[[10,12,'  '],[22,24,'  ']])
        self.assertEqual(bim['span'][:2],[34,44])
        wrong=self.fixture('published-280');outer=wrong['integrated']['nodes'][0]['children'][0]
        outer['span']=[0,10,'- [x] foo\n'];outer['facts']['item']=outer['span']
        self.reject_fixture(wrong,'outside owning envelope')
        wrong=self.fixture('published-280');wrong['integrated']['nodes'][0]['children'][0]['facts']['item']=[0,10,'- [x] foo\n']
        self.reject_fixture(wrong,'whole owning node envelope')
        for i,start in enumerate((10,22)):
            wrong=self.fixture('published-280');node=wrong['integrated']['nodes'][0]['children'][0]['children'][0]['children'][i]
            end=node['span'][1];raw=wrong['source'].encode()[start:end].decode();node['span']=[start,end,raw];node['facts']['item']=node['span']
            if i==0:
                nested=wrong['integrated']['nodes'][0]['children'][0]['children'][0]
                nested['span']=[10,34,wrong['source'].encode()[10:34].decode()]
            self.reject_fixture(wrong,'own list marker')
        self.assertEqual(self.fixtures['combined-all-five']['integrated']['nodes'][0]['span'],[0,21,'- [x] ~~www.a.org~~\n\n'])

    def test_repaired_reference_owner_and_terminal_content_audit(self):
        for scope in ('core','integrated'):
            fixture=self.fixtures['modern-reference-fold'];node=fixture[scope]['nodes'][0]
            self.assertEqual(node['span'],[0,6,'[ẞ]\n']);self.assertEqual(node['content'],[[0,5,'[ẞ]']])
            link=node['children'][0];self.assertEqual(link['span'],[0,5,'[ẞ]'])
            self.assertEqual(link['facts']['visible'],[1,4,'ẞ'])
            self.assertEqual(link['facts']['binding'],{'definition':[7,21,'[ss]: /target\n'],'label':[8,10,'ss'],'destination':[13,20,'/target'],'normalized_label':'ss'})
            wrong=self.fixture('modern-reference-fold');wrong[scope]['nodes'][0]['span']=[0,21,fixture['source']]
            self.reject_fixture(wrong,'cannot cross a blank line')
            wrong=self.fixture('modern-reference-fold');wrong[scope]['nodes'][0]['content']=[[0,21,fixture['source']]]
            self.reject_fixture(wrong,'outside owning envelope')
        for identity in ('published-198','modern-source','table-code-pipe-crlf'):
            wrong=self.fixture(identity);n=wrong['core']['nodes'][0];n['content']=[n['span']]
            self.reject_fixture(wrong,'exclude the terminal block ending')
        for scope in ('core','integrated'):
            code=self.fixtures['block-code-protection'][scope]['nodes'][0]
            self.assertEqual(code['content'],[[4,35,'- [x] ~~a~~ www.a.org <script>\n']])
            nodes=self.fixtures['published-324'][scope]['nodes']
            self.assertEqual([n['span'][:2] for n in nodes],[[0,18],[19,30],[31,41],[42,66]])
            self.assertTrue(all('\n' not in n['span'][2] for n in nodes))
        wrong=self.fixture('block-code-protection');wrong['core']['nodes'][0]['content']=[wrong['core']['nodes'][0]['span']]
        self.reject_fixture(wrong,'exclude opening and closing fences')
        wrong=self.fixture('published-324');wrong['core']['nodes']=[{'kind':'text','span':[0,66,wrong['source'].rstrip('\n')],'content':[[0,66,wrong['source'].rstrip('\n')]],'children':[],'facts':{'value':wrong['source'].rstrip('\n')}}]
        self.reject_fixture(wrong,'physical line endings belong to break/block syntax')
        wrong=self.fixture('task-uppercase-crlf');wrong['integrated']['nodes'][0]['children'][0]['children'][0]['children'][0]['facts']['ending']=[11,12,'\n']
        self.reject_fixture(wrong,'separate original block-ending metadata')
        wrong=self.fixture('task-uppercase-crlf');wrong['integrated']['nodes'][0]['content']=[[8,10,'é']]
        self.reject_fixture(wrong,'payload belongs to child syntax')
        for scope in ('core','integrated'):
            node=self.fixtures['modern-control-destination'][scope]['nodes'][0]
            self.assertEqual(node['span'],[0,8,'[a](x\x0by)'])
            self.assertEqual(node['facts']['value'],'[a](x\x0by)')

    def test_contextual_errors_are_read_only(self):
        def inventory(path):
            return {str(f.relative_to(path)):(f.read_bytes(),f.stat().st_mtime_ns) for f in path.rglob('*') if f.is_file()}
        original=inventory(p.PROFILE)
        for identity,scope,reason in [('published-649','core','original raw bytes differ'),('task-uppercase-crlf','integrated','UTF-8 boundary')]:
            with tempfile.TemporaryDirectory() as temporary:
                target=Path(temporary)/'profile';shutil.copytree(p.PROFILE,target)
                data=json.loads((target/'expectations.json').read_bytes());f=next(x for x in data['fixtures'] if x['id']==identity)
                if scope=='core':f[scope]['nodes'][0]['span'][1]-=1
                else:f[scope]['nodes'][0]['children'][0]['children'][0]['content']=[[8,9,'é']]
                (target/'expectations.json').write_bytes(p.serialize(data));before=inventory(target)
                import contextlib,io
                stderr=io.StringIO()
                with patch.object(p,'PROFILE',target),patch.object(sys,'argv',['gfm_profile.py','--check']),contextlib.redirect_stderr(stderr):
                    self.assertEqual(p.main(),1)
                message=stderr.getvalue();self.assertIn(identity+' '+scope,message);self.assertIn('nodes[0]',message);self.assertIn(reason,message)
                if scope=='integrated':self.assertIn('children[0]',message)
                self.assertEqual(inventory(target),before)
        self.assertEqual(inventory(p.PROFILE),original)

    def test_ledger_negative_controls(self):
        mutations = [
            (lambda d:d['cases'].pop(), 'case IDs'),
            (lambda d:d['cases'][0]['expectations'].pop('core'), 'scopes'),
            (lambda d:d['cases'][0].update(scope='bogus'), 'identity/hash/section/scope'),
            (lambda d:d['extensions'].append('footnotes'), 'extensions'),
            (lambda d:d['cases'][0].update(input_sha256='0'*64), 'identity/hash/section/scope'),
            (lambda d:d['cases'][0]['expectations']['core'].update(original=999), 'dangling'),
            (lambda d:d['cases'][648]['expectations']['core'].update(fixture='missing'), 'dangling'),
            (lambda d:d['cases'][0].update(ignored=True), 'unknown fields'),
            (lambda d:d['cases'][0].update(example=True), 'case IDs|identity/hash'),
        ]
        for mutation, message in mutations:
            with self.subTest(message=message):
                data=copy.deepcopy(self.ledger); mutation(data)
                with self.assertRaisesRegex(ValueError, message):
                    p.validate_ledger(data,self.originals,self.fixtures)

    def test_provenance_and_schema_negative_controls(self):
        for key in ('reason','spec'):
            f=self.fixture('published-649'); f.pop(key)
            self.reject_fixture(f,'missing or unknown fields')
        f=self.fixture('published-649');f['reason']='';self.reject_fixture(f,'reason')
        f=self.fixture('published-649');f['spec']['pin']='0'*64;self.reject_fixture(f,'spec pin')
        f=self.fixture('published-649');f['integrated']['nodes'][0]['facts']['ignored']=True;self.reject_fixture(f,'unknown fields')
        f=self.fixture('published-649');f['integrated']['nodes'][0]['span'][1]-=1;self.reject_fixture(f,'raw bytes differ')
        f=self.fixture('published-649');f['integrated']['nodes'][0]['span']=[9,1,''];self.reject_fixture(f,'reversed')
        f=self.fixture('published-649');f['frontmatter']=True;self.reject_fixture(f,'frontmatter')

    def test_semantic_task_unicode_and_positions_controls(self):
        f=self.fixture('task-uppercase-crlf');f['integrated']['nodes'][0]['children'][0]['children'][0]['children'][0]['content'][0]=[8,9,'é'];self.reject_fixture(f,'UTF-8 boundary')
        f=self.fixture('task-uppercase-crlf');f['integrated']['nodes'][0]['children'][0]['children'][0]['facts']['check'][2]='x';self.reject_fixture(f,'raw bytes differ')
        f=self.fixture('task-uppercase-crlf');f['integrated']['nodes'][0]['children'][0]['children'][0]['facts']['checked']=False;self.reject_fixture(f,'checked state')
        f=self.fixture('task-uppercase-crlf');f['integrated']['nodes'][0]['facts']['positions'][0]['column']=7;self.reject_fixture(f,'scalar coordinates')

    def test_semantic_table_controls_beyond_hash(self):
        f=self.fixture('published-204');r=f['integrated']['nodes'][0]['facts']['rows'];r[2]['padding'][0]['span']=[32,32,''];self.reject_fixture(f,'padding must be spanless')
        f=self.fixture('published-204');r=f['integrated']['nodes'][0]['facts']['rows'];r[3]['cells'].pop();r[3]['source_count']=2;self.reject_fixture(f,'including excess cells')
        f=self.fixture('published-199');f['integrated']['nodes'][0]['facts']['rows'][0]['alignment'][0]='left';self.reject_fixture(f,'alignment differs')
        f=self.fixture('published-200');f['integrated']['nodes'][0]['facts']['rows'][0]['escaped_pipes']=[];self.reject_fixture(f,'escaped-pipe inventory')

    def test_semantic_strike_and_synthetic_controls(self):
        f=self.fixture('published-491');f['integrated']['nodes'][0]['facts']['arity']=1;self.reject_fixture(f,'arity/spelling')
        f=self.fixture('published-622');f['integrated']['nodes'][0]['facts']['parts'][0]['source']=[0,0,''];self.reject_fixture(f,'prefix must be spanless')
        f=self.fixture('filter-script');f['integrated']['nodes'][1]['facts']['name']='style';self.reject_fixture(f,'name/boundary')
        f=self.fixture('filter-script');f['integrated']['nodes'][1]['facts']['protected']=[0,0,''];self.reject_fixture(f,'outside protected')

    def test_fake_consumer_controls_and_complete_refusal(self):
        expected=self.fixtures['task-uppercase-crlf']['integrated']
        wrong=dict(status='pass',html='wrong',nodes=expected['nodes'],error=None)
        with self.assertRaisesRegex(ValueError,'false pass'):
            p.consumer_result(expected,wrong)
        wrong['html']=expected['html'];wrong['nodes']=copy.deepcopy(expected['nodes']);wrong['nodes'][0]['children'][0]['children'][0]['children'][0]['content'][0][1]=9
        with self.assertRaisesRegex(ValueError,'source structure mismatch'):
            p.consumer_result(expected,wrong)
        self.assertEqual(p.consumer_result(expected,dict(status='unimplemented',html=None,nodes=None,error=None)),'unimplemented')
        with self.assertRaisesRegex(ValueError,'cannot claim produced output'):
            p.consumer_result(expected,dict(status='unimplemented',html=expected['html'],nodes=expected['nodes'],error=None))
        with self.assertRaisesRegex(ValueError,'subset'):
            p.results(self.ledger,self.originals,self.fixtures,selected=['published:1:core'],require_complete=True)
        with self.assertRaisesRegex(ValueError,'missing actual'):
            p.results(self.ledger,self.originals,self.fixtures,require_complete=True)
        actual=p.results(self.ledger,self.originals,self.fixtures)
        supplements=sum(not k.startswith('published-') for k in self.fixtures)
        self.assertEqual(len(actual),677*3+2*supplements)
        self.assertEqual(set(actual.values()),{'not-run'})
        unrun={key:dict(status='not-run',html=None,nodes=None,error=None) for key in actual}
        with self.assertRaisesRegex(ValueError,'not-run, unimplemented'):
            p.results(self.ledger,self.originals,self.fixtures,actual=unrun,require_complete=True)

    def test_code_pipe_semantics_are_independent(self):
        f=self.fixture('published-200')
        self.assertEqual(f['core']['nodes'][0]['children'][0]['facts']['literal'], '\\|')
        self.assertEqual(f['integrated']['nodes'][0]['children'][0]['facts']['literal'], '|')
        f['integrated']['nodes'][0]['children'][0]['facts']['literal']='\\|'
        self.reject_fixture(f,'semantic literal differs')

    def test_discontinuous_absent_zero_width_and_ignored_assertion(self):
        n=self.fixtures['strike-container-gaps']['integrated']['nodes'][0]
        self.assertEqual(n['content'],[])
        self.assertEqual(n['children'][0]['content'],[[4,7,'é\n'],[9,12,'fin']])
        p.source_span('é',[0,0,''])
        with self.assertRaisesRegex(ValueError,'expected \[start,end,raw\]'):
            p.source_span('é',None)
        f=self.fixture('strike-container-gaps');f['integrated']['nodes'][0]['children'][0]['content'].reverse()
        self.reject_fixture(f,'out of order')
        f=self.fixture('published-491');f['integrated']['nodes'][0]['facts']['noop_assertion']='ignored'
        self.reject_fixture(f,'unknown fields')

    def test_actual_artifact_corruption_and_offline_regeneration(self):
        def inventory(path):
            return {str(f.relative_to(path)):(f.read_bytes(),f.stat().st_mtime_ns) for f in path.rglob('*') if f.is_file()}
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary)/'repo';(root/'tools').mkdir(parents=True)
            for name in ('gfm_corpus.py','gfm_profile.py'):
                shutil.copy2(p.ROOT/'tools'/name,root/'tools'/name)
            shutil.copytree(p.ROOT/'internal/parser/testdata/gfm',root/'internal/parser/testdata/gfm')
            core=root/'internal/parser/testdata/commonmark/0.31.2/upstream';core.mkdir(parents=True)
            shutil.copy2(p.ROOT/'internal/parser/testdata/commonmark/0.31.2/upstream/spec.txt',core/'spec.txt')
            tool=root/'tools/gfm_profile.py'
            def invoke(*args):
                bootstrap="import socket,runpy,sys; socket.socket=lambda *a,**k: (_ for _ in ()).throw(AssertionError('network forbidden')); sys.path.insert(0,sys.argv[1]); sys.argv=sys.argv[2:]; runpy.run_path(sys.argv[0],run_name='__main__')"
                return subprocess.run([sys.executable,'-B','-c',bootstrap,str(root/'tools'),str(tool),*args],cwd=temporary,env=dict(os.environ,PATH=''),capture_output=True,text=True,check=False)
            before=inventory(root)
            result=invoke('--check');self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(inventory(root),before)
            ledger=root/'internal/parser/testdata/gfm/profile/ledger.json';expected=ledger.read_bytes()
            untouched={k:v for k,v in before.items() if not k.endswith('/ledger.json')}
            for _ in range(2):
                ledger.unlink();result=invoke();self.assertEqual(result.returncode,0,result.stderr);self.assertEqual(ledger.read_bytes(),expected)
                self.assertEqual({k:v for k,v in inventory(root).items() if not k.endswith('/ledger.json')},untouched)
            original=root/'internal/parser/testdata/gfm/0.29-gfm/examples.json';data=json.loads(original.read_bytes());data[0]['html']='CORRUPT';original.write_bytes(p.serialize(data));before=inventory(root)
            result=invoke('--check');self.assertEqual(result.returncode,1);self.assertIn('immutable original corpus drift',result.stderr);self.assertEqual(inventory(root),before)

    def test_offline_read_only_and_determinism(self):
        def inventory(path):
            return {str(f.relative_to(path)):(f.read_bytes(),f.stat().st_mtime_ns) for f in path.rglob('*') if f.is_file()}
        before=inventory(p.PROFILE)
        with patch.object(socket,'socket',side_effect=AssertionError('network forbidden')):
            a,b=p.load();one=p.serialize(p.ledger(a,b));two=p.serialize(p.ledger(*p.load()))
        self.assertEqual(one,two)
        self.assertEqual(one,(p.PROFILE/'ledger.json').read_bytes())
        self.assertEqual(before,inventory(p.PROFILE))
        with tempfile.TemporaryDirectory() as temp:
            target=Path(temp)/'profile';shutil.copytree(p.PROFILE,target)
            with patch.object(p,'PROFILE',target):
                data=json.loads((target/'ledger.json').read_bytes());data['cases'].pop();(target/'ledger.json').write_bytes(p.serialize(data));before=inventory(target)
                with patch.object(sys,'argv',['gfm_profile.py','--check']):
                    self.assertEqual(p.main(),1)
                self.assertEqual(before,inventory(target))
        run=subprocess.run([sys.executable,'-B',str(p.ROOT/'tools/gfm_profile.py'),'--check'],cwd=tempfile.gettempdir(),capture_output=True,text=True,check=False)
        self.assertEqual(run.returncode,0,run.stderr)


if __name__ == '__main__':
    unittest.main()
