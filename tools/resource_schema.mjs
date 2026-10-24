// Required JSON Schema pattern-dialect gate. Uses Node built-ins only; no npm
// package or production runtime dependency. The Go suite uses the same fixture.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';

const schemaURL = process.argv[2] ?? new URL('../internal/resource/supplied.schema.json', import.meta.url);
const schema = JSON.parse(readFileSync(schemaURL, 'utf8'));
const cases = JSON.parse(readFileSync(new URL('../internal/resource/testdata/supplied-paths.json', import.meta.url), 'utf8'));
assert.ok(cases.length > 0, 'path parity fixtures must not be empty');
const branches = schema.properties.entries.items.oneOf;
assert.equal(branches.length, 2, 'both file and non-file schema branches must be checked');
let checked = 0;
for (const [index, branch] of branches.entries()) {
  const path = branch.properties.path;
  assert.equal(path.type, 'string');
  assert.equal(path.minLength, 1);
  // Exercise ECMA-262 both with and without Unicode mode. Python jsonschema's
  // regex dialect differs for U+2028/U+2029 and cannot replace this check.
  for (const flags of ['', 'u']) {
    const pattern = new RegExp(path.pattern, flags);
    for (const fixture of cases) {
      assert.equal(pattern.test(fixture.path), fixture.valid,
        `branch=${index} flags=${JSON.stringify(flags)} path=${JSON.stringify(fixture.path)}`);
      checked++;
    }
  }
}
process.stdout.write(`ECMAScript resource schema: ${checked} path checks passed\n`);
