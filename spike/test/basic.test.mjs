// Q1 loading, Q2 dynamic symbols, Q3 dispatch/panics/errors, Q4 Promise from goroutines.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { load } from './helpers.mjs'

const b = load('basic')

test('Q1: c-shared .node addon loads via require() and exposes exports', () => {
  assert.equal(b.hello(), 'hello from Go')
  assert.equal(typeof b.add, 'function')
  assert.ok(Object.keys(b).length >= 30)
})

test('Q2: all required symbols resolved at runtime, optional symbols detected', () => {
  const i = b.info()
  assert.ok(i.requiredSymbols > 30)
  assert.ok(i.napiVersion >= 8)
  const major = Number(process.versions.node.split('.')[0])
  if (major >= 18) assert.equal(i.hasSymbolFor, true) // node_api_symbol_for: Node-API 9
  // napi_node_version only exposes major.minor.patch: on a prerelease (e.g. "27.0.0-alpha.0.0.0")
  // the suffix is omitted.
  assert.equal(i.nodeVersion, process.versions.node.replace(/-.*$/, ''))
})

test('Q3: proper dispatch across many functions ("static dataCopy" bug from earlier attempts)', () => {
  for (let i = 0; i < 12; i++) assert.equal(b[`f${i}`](), i)
})

test('Q3: arguments beyond the inline buffer (8) and missing arguments', () => {
  const many = Array.from({ length: 20 }, (_, i) => i + 1)
  assert.equal(b.countArgs(...many), 20)
  assert.equal(b.sumArgs(...many), 210)
  assert.equal(b.countArgs(), 0)
  assert.throws(() => b.add(1), /A number was expected/) // missing argument = undefined
})

test('Q3: string conversions (ASCII, UTF-8, empty, > stack buffer)', () => {
  assert.equal(b.echo(''), '')
  assert.equal(b.echo('àèìòù 😀 漢字'), 'àèìòù 😀 漢字')
  const big = 'x'.repeat(100_000) + '😀'
  assert.equal(b.echo(big), big)
  assert.equal(b.concat('a\u0000b', 'c'), 'a\u0000bc') // embedded NUL bytes preserved (ptr+len)
})

test('Q3: Go panic → JS Error GO_PANIC, process survives', () => {
  assert.throws(() => b.panicNow('x'), { code: 'GO_PANIC', message: 'boom: x' })
  assert.throws(() => b.panicNil(), (e) => e.code === 'GO_PANIC' && /nil map/.test(e.message))
  assert.equal(b.add(1, 2), 3)
})

test('Q3: Go error → JS exception with code; napi status → Node message', () => {
  assert.throws(() => b.fail('bad'), { code: 'E_TEST', message: 'bad' })
  assert.throws(() => b.wrongType('s'), /A number was expected/)
})

test('Q3: Go → JS → Go reentrancy and JS exceptions propagating through Go', () => {
  assert.equal(b.callMe((x) => b.add(x, 1), 41), 42)
  assert.throws(() => b.callMe(() => { throw new TypeError('from JS') }, 0), { name: 'TypeError', message: 'from JS' })
  assert.deepEqual(b.makeObject(4), { name: 'squares', values: [0, 1, 4, 9] })
  assert.deepEqual(b.makeObject(0), { name: 'squares', values: [] }) // empty array
  assert.throws(() => b.makeObject(null), /A number was expected/)
  assert.throws(() => b.makeObject(), /A number was expected/)
})

test('Q4: Promise resolved on the JS thread, work executed on a Go thread', async () => {
  const r = await b.sleep(10)
  assert.equal(r.ms, 10)
  assert.equal(r.resolveThread, r.jsThread)
  assert.notEqual(r.workThread, r.jsThread)
})

test('Q4: Promise with zero wait and invalid arguments', async () => {
  assert.equal((await b.sleep(0)).ms, 0)
  assert.throws(() => b.sleep('x'), /A number was expected/) // synchronous error, no Promise
  assert.throws(() => b.sleep(undefined), /A number was expected/)
})

test('Q4: async rejection and panic', async () => {
  await assert.rejects(b.rejectAsync('nope'), { code: 'E_ASYNC', message: 'nope' })
  await assert.rejects(b.panicAsync(), { code: 'GO_PANIC', message: 'async boom' })
})

test('Q4: 20,000 concurrent Promises from goroutines', async () => {
  const N = 20_000
  const runsBefore = b.threadInfo().dispatchRuns
  const res = await Promise.all(Array.from({ length: N }, (_, i) => b.sleep(i % 5)))
  assert.equal(res.length, N)
  assert.ok(res.every((r) => r.resolveThread === r.jsThread))
  // coalescing: multiple resolutions per dispatcher execution
  assert.ok(b.threadInfo().dispatchRuns - runsBefore < N)
})

test('Q4: parallel work across multiple goroutines', async () => {
  const s = await b.parallelSum(8, 1_000_000)
  assert.equal(typeof s, 'number')
})

test('Q4: repeated JS calls from a goroutine via the dispatcher', async () => {
  const seen = []
  globalThis.__napiGoTick = (i) => seen.push(i)
  const errs = await b.tick(1000)
  // posted callbacks arrive before resolution: same dispatcher, FIFO order
  assert.equal(errs, 0)
  assert.equal(seen.length, 1000)
  assert.deepEqual(seen.slice(0, 5), [0, 1, 2, 3, 4])
  delete globalThis.__napiGoTick
})

test('Q8: typed path — conversions in C, edge cases, and errors', () => {
  assert.equal(b.addTyped(1.5, 2), 3.5)
  assert.ok(Object.is(b.addTyped(-0, -0), -0))
  assert.equal(b.addTyped(Number.MAX_VALUE, Number.MAX_VALUE), Infinity)
  assert.equal(b.echoTyped(''), '')
  assert.equal(b.echoTyped('àé😀\u0000z'), 'àé😀\u0000z')
  const long = 'y'.repeat(5000) // exceeds C buffer (512): slow path on both input and output
  assert.equal(b.echoTyped(long), long)
  assert.equal(b.repeatTyped('ab', 0), '')
  assert.equal(b.repeatTyped('x', 1000), 'x'.repeat(1000))
  assert.equal(b.notTyped(false), true)
  assert.throws(() => b.addTyped('a', 1), { name: 'TypeError', message: 'A number was expected' })
  assert.throws(() => b.addTyped(1), { name: 'TypeError' }) // missing argument
  assert.throws(() => b.echoTyped(null), { name: 'TypeError', message: 'A string was expected' })
  assert.throws(() => b.notTyped(1), { name: 'TypeError', message: 'A boolean was expected' })
  assert.throws(() => b.repeatTyped('a', -1), { code: 'E_RANGE' })
  assert.throws(() => b.panicTyped(1), { code: 'GO_PANIC', message: 'typed boom' })
})
