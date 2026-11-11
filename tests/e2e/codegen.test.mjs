// End-to-end tests of the code generator (napi-go gen, internal/codegen) on the addon tests/e2e/addons/codegen,
// whose exports are declared with //napi: annotations only: behavior of the generated
// registrations, the JS entry point and the TypeScript declarations (checked with tsc).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { copyFileSync, mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { load, addonPath, nodeEval, lastJSONLine } from './helpers.mjs'

const A = load('codegen')
const here = dirname(fileURLToPath(import.meta.url))
const addonDir = join(here, 'addons', 'codegen')

test('exports: names, functions, constants and errors', () => {
  assert.deepEqual(Object.keys(A).sort(), ['Cart', 'MAX_ITEMS', 'Session', 'add', 'callback', 'checksum', 'count',
    'describe', 'encode', 'findUser', 'greet', 'lookup', 'mirror', 'openSession', 'parsePort', 'touch', 'version'])
  assert.equal(A.add(1, 2), 3)
  assert.equal(A.parsePort(8080), 8080)
  assert.throws(() => A.parsePort(0), { name: 'RangeError', code: 'E_PORT', message: 'invalid port 0' })
  assert.equal(A.version, '2.0.0')
  assert.equal(A.MAX_ITEMS, 3)
  assert.equal(A.greet({ id: 1, name: 'ann', city: 'Rome', tags: ['a', 'b'], password: 'ignored' }), 'hello ann from Rome!!')
  assert.throws(() => A.greet({ id: 1, name: 'ann' }), { code: 'ERR_INVALID_ARG_TYPE', message: 'argument 1: city: expected string, got undefined' })
  assert.equal(A.checksum(Buffer.from([1, 2, 3])), 6)
  assert.equal(A.describe('x', new Date(0), 5n), 'string 1970 5')
  assert.equal(A.describe(1, new Date(0), null), 'number 1970 <nil>')
  assert.deepEqual([...A.encode('ab', 2n)], [97, 98, 97, 98])
  assert.deepEqual(A.lookup({ 1: 'a', 2: 'b' }, [2, 3], { upper: true }), ['B', false])
  assert.equal(A.count(1, 2, 3), 3)
  assert.equal(A.callback((x) => x * 2, 21), 42)
  assert.deepEqual(A.mirror([1, -2]), [-1, 2])
})

test('asynchronous functions return Promises', async () => {
  assert.deepEqual(await A.findUser('bob'), { id: 1, name: 'bob', email: null, city: 'Rome' })
  assert.equal(await A.findUser('nobody'), null)
  await assert.rejects(A.findUser(''), { message: 'empty name' })
  assert.equal(await A.touch(1), undefined)
})

test('classes: constructor, methods, accessors, statics, skipped and internal methods', () => {
  const c = new A.Cart('ann')
  const pen = { name: 'pen', price: 1.5, quantity: 2 }
  assert.equal(c.add(pen), c, 'add returns the same instance')
  assert.equal(c.total(), 3)
  assert.deepEqual(c.items(), [pen])
  assert.equal(c.owner, 'ann')
  c.owner = 'bob'
  assert.equal(c.owner, 'bob')
  assert.equal(c.size, 1)
  assert.throws(() => { 'use strict'; c.size = 5 }, TypeError)
  c.add(pen).add(pen)
  assert.throws(() => c.add(pen), { name: 'RangeError', code: 'E_FULL' })
  const other = A.Cart.of('eve', pen)
  assert.ok(other instanceof A.Cart)
  c.clear()
  c.mergeFrom(other)
  c.mergeFrom(null)
  assert.equal(c.size, 1)
  assert.equal(other.size, 0)
  assert.equal(A.Cart.CAPACITY, A.MAX_ITEMS)
  assert.throws(() => new A.Cart(''), { name: 'TypeError', code: 'E_OWNER' })
  assert.deepEqual(Object.getOwnPropertyNames(A.Cart.prototype).sort(),
    ['add', 'clear', 'constructor', 'items', 'mergeFrom', 'owner', 'size', 'total'],
    'Secret (//napi:skip), Finalize and unexported methods are not exported')
  const s = A.openSession(7)
  assert.ok(s instanceof A.Session)
  assert.equal(s.id(), 7)
  assert.throws(() => new A.Session(), { message: 'Class Session cannot be constructed from JS' })
})

test('the generated JS entry point loads the addon next to it', () => {
  const dir = mkdtempSync(join(tmpdir(), 'napi-go-codegen-'))
  try {
    copyFileSync(join(addonDir, 'index.js'), join(dir, 'index.js'))
    copyFileSync(addonPath('codegen'), join(dir, 'codegen.node'))
    // In a child process: a loaded addon cannot be deleted on Windows.
    const r = nodeEval(`process.stdout.write(JSON.stringify({ sum: require(${JSON.stringify(join(dir, 'index.js'))}).add(2, 3) }))`)
    assert.equal(r.status, 0, r.stderr)
    assert.deepEqual(lastJSONLine(r), { sum: 5 })
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})

// The TypeScript compiler is an optional tool: without it the test is skipped, unless the
// environment (CI) requires it.
const requireTsc = process.env.NAPI_GO_REQUIRE_TSC === '1'
const tsc = spawnSync('tsc', ['--version'], { encoding: 'utf8', shell: process.platform === 'win32' })
const hasTsc = !tsc.error && tsc.status === 0

test('the generated declarations type-check correct uses and reject wrong ones (tsc)', { skip: !hasTsc && !requireTsc && 'tsc is not installed' }, () => {
  assert.ok(hasTsc, 'NAPI_GO_REQUIRE_TSC=1 but tsc cannot run')
  const r = spawnSync('tsc', ['-p', join(here, 'codegen-types')], { encoding: 'utf8', shell: process.platform === 'win32' })
  assert.equal(r.status, 0, r.stdout + r.stderr)
})

test('the declarations document the exports with the Go comments', () => {
  const dts = readFileSync(join(addonDir, 'index.d.ts'), 'utf8')
  assert.match(dts, /\/\*\* Add returns the sum of a and b\. \*\/\nexport declare function add\(a: number, b: number\): number/)
  assert.match(dts, /\/\*\*\n \* ParsePort checks that port is a valid TCP port\.\n \* It throws a RangeError otherwise\.\n \*\//)
  assert.match(dts, /  \/\*\* ZIP is the postal code\. \*\/\n  zip\?: string/)
  assert.doesNotMatch(dts, /napi:/, 'annotations are not part of the documentation')
})
