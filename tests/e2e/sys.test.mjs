// End-to-end tests for Layer 0 (package sys) through the tests/e2e/addons/sysbasic addon.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Worker } from 'node:worker_threads'
import {
  load, addonPath, isWin, isDarwinX64, hostNapiVersion, nodeEval, lastJSONLine, signalChild,
} from './helpers.mjs'

const A = load('sysbasic')

const DEFAULT_DECLARED_VERSION = 8
const V10 = 10
const V9 = 9
const INLINE_ARGS = 8 // GN_INLINE_ARGS in sys/internal.h
const MANY_ARGS = 100
const STACK_STRING_MAX = 64 // stackStringMax in the addon
const LONG_STRING_LENGTH = 100_000
const EXTERNALS = 1000
const TSFN_CALLS = 1000
const ASYNC_N = 1_000_000
const CONCURRENT_JOBS = 16
const WORKERS = 4
const WORKER_SUM_N = 10
const SETTLE_MS = 50
const POLL_LIMIT_MS = 10_000
const SIGNAL_HANG_MS = 5_000
const SIGNAL_CHILD_LIFETIME_MS = 15_000

// 0 + 1 + ... + (n-1); for n = 0 the formula would yield -0.
const sumTo = (n) => (n > 0 ? (n * (n - 1)) / 2 : 0)

async function waitFor (predicate, what) {
  const deadline = Date.now() + POLL_LIMIT_MS
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for: ${what}`)
    await new Promise((resolve) => setTimeout(resolve, SETTLE_MS))
  }
}

test('module loads and declares Node-API 8 by default', () => {
  assert.equal(A.name(), 'sysbasic')
  assert.equal(A.declaredVersion(), DEFAULT_DECLARED_VERSION)
  assert.equal(A.moduleAPIVersion(), DEFAULT_DECLARED_VERSION)
})

test('build tag napi_go_v10: module declares Node-API 10', { skip: hostNapiVersion < V10 && 'Node without Node-API 10' }, () => {
  // In a separate process: two Go addons in the same process are not supported on macOS x64.
  const r = nodeEval(`const a = require(${JSON.stringify(addonPath('sysbasic-v10'))});
    process.stdout.write(JSON.stringify({ name: a.name(), declared: a.declaredVersion(), module: a.moduleAPIVersion() }))`)
  assert.deepEqual(lastJSONLine(r), { name: 'sysbasic-v10', declared: V10, module: V10 })
})

test('runtime-resolved functions and optional functions', () => {
  assert.equal(A.available('napi_create_int32'), true)
  assert.equal(A.available('node_api_symbol_for'), hostNapiVersion >= V9)
  assert.equal(A.available('node_api_create_property_key_utf8'), hostNapiVersion >= V10)
  assert.throws(() => A.available('napi_nonexistent'), TypeError)
  assert.throws(() => A.available(''), TypeError)
  assert.throws(() => A.available(null), TypeError)
  assert.throws(() => A.available(), TypeError)
})

test('numbers: boundary values and wrong types', () => {
  assert.equal(A.add(1.5, 2.5), 4)
  assert.ok(Object.is(A.add(-0, -0), -0))
  assert.ok(Number.isNaN(A.add(NaN, 1)))
  assert.equal(A.add(Number.MAX_VALUE, Number.MAX_VALUE), Infinity)
  assert.equal(A.add(Number.MIN_VALUE, 0), Number.MIN_VALUE)
  assert.throws(() => A.add(1), TypeError)
  assert.throws(() => A.add(null, 1), TypeError)
  assert.throws(() => A.add('1', 2), TypeError)
})

test('strings: empty, UTF-8, stack buffer boundary, long', () => {
  assert.equal(A.echo(''), '')
  assert.equal(A.echo('ascii'), 'ascii')
  assert.equal(A.echo('€😀 àèìòù'), '€😀 àèìòù')
  for (const n of [STACK_STRING_MAX - 1, STACK_STRING_MAX, STACK_STRING_MAX + 1]) {
    const s = 'x'.repeat(n)
    assert.equal(A.echo(s), s, `length ${n}`)
  }
  const long = 'é'.repeat(LONG_STRING_LENGTH)
  assert.equal(A.echo(long), long)
  assert.throws(() => A.echo(null), TypeError)
  assert.throws(() => A.echo(1), TypeError)
  assert.throws(() => A.echo(), TypeError)
})

test('arguments: none, inline buffer boundary, many', () => {
  assert.equal(A.countArgs(), 0)
  for (const n of [INLINE_ARGS - 1, INLINE_ARGS, INLINE_ARGS + 1, MANY_ARGS]) {
    assert.equal(A.countArgs(...Array(n).fill(0)), n, `${n} arguments`)
  }
  const values = Array.from({ length: MANY_ARGS }, (_, i) => i)
  assert.equal(A.sumArgs(...values), sumTo(MANY_ARGS))
  assert.equal(A.sumArgs(), 0)
  assert.throws(() => A.sumArgs(1, 'x'), TypeError)
  const obj = { f: A.thisValue }
  assert.equal(obj.f(), obj)
})

test('errors: panic, codes, extended error info', () => {
  assert.throws(() => A.panicNow('x'), { code: 'GO_PANIC', message: /boom: x/ })
  assert.throws(() => A.panicNow(), { code: 'GO_PANIC', message: /boom: $/ })
  assert.equal(A.add(1, 2), 3, 'module works after a panic')
  assert.throws(() => A.throwCoded('E_TEST', 'message'), { code: 'E_TEST', message: 'message' })
  assert.throws(() => A.throwCoded('E_TEST'), TypeError)
  assert.deepEqual(A.lastError(), {
    status: 'napi_number_expected',
    infoStatus: 'napi_number_expected',
    message: 'A number was expected',
  })
})

test('JS → Go → JS → Go reentrancy and JS exceptions through Go', () => {
  assert.equal(A.callMe((x) => x * 2, 21), 42)
  assert.equal(A.callMe(() => A.callMe(() => A.add(1, 2))), 3)
  assert.equal(A.callMe(() => undefined), undefined)
  assert.throws(() => A.callMe(() => { throw new RangeError('from JS') }), { name: 'RangeError', message: 'from JS' })
  assert.throws(() => A.callMe(), TypeError)
})

test('type tag: tagged objects, double tagging, primitives, null', () => {
  const obj = {}
  assert.equal(A.tagObject(obj, 1, 2), 'napi_ok')
  assert.equal(A.tagObject(obj, 1, 2), 'napi_invalid_arg')
  assert.equal(A.checkTag(obj, 1, 2), true)
  assert.equal(A.checkTag(obj, 1, 3), false)
  assert.equal(A.checkTag({}, 1, 2), false)
  // Node boxes primitives into objects (ToObject): the tag lands on a temporary wrapper.
  assert.equal(A.tagObject(1, 0, 0), 'napi_ok')
  assert.throws(() => A.tagObject(null, 0, 0), TypeError)
  assert.throws(() => A.tagObject(undefined, 0, 0), TypeError)
  assert.throws(() => A.tagObject(obj), TypeError)
})

test('PropertyDescriptor in Go memory with runtime.Pinner', () => {
  const obj = A.defineProps({})
  assert.equal(obj.hello(), 'hello')
  assert.deepEqual(Object.getOwnPropertyDescriptor(obj, 'answer'), {
    value: 42, writable: false, enumerable: true, configurable: false,
  })
  assert.equal(Object.getOwnPropertyDescriptor(obj, 'hello').enumerable, false)
  assert.throws(() => A.defineProps(), TypeError)
})

test('Node version and Node-API 9/10 functions', () => {
  const v = A.nodeVersion()
  // napi_node_version exposes only major.minor.patch: on a prerelease (e.g. "27.0.0-alpha.0.0.0")
  // the suffix is omitted.
  assert.equal(`${v.major}.${v.minor}.${v.patch}`, process.versions.node.replace(/-.*$/, ''))
  assert.equal(v.release, 'node')
  if (hostNapiVersion >= V9) {
    assert.equal(A.symbolFor('x'), Symbol.for('x'))
    assert.equal(A.symbolFor(''), Symbol.for(''))
    const file = A.moduleFileName()
    assert.ok(file.startsWith('file://') && file.endsWith('sysbasic.node'), file)
  } else {
    assert.equal(A.moduleFileName(), undefined)
  }
  if (hostNapiVersion >= V10) {
    assert.equal(A.propertyKey('k'), 'k')
    assert.equal(A.propertyKey(''), '')
  }
})

test('finalizer: Externals collected by V8 GC', () => {
  const r = nodeEval(`const a = require(${JSON.stringify(addonPath('sysbasic'))});
    a.makeExternals(0);
    a.makeExternals(${EXTERNALS});
    const deadline = Date.now() + ${POLL_LIMIT_MS};
    (function poll () {
      gc();
      if (a.finalizedCount() >= ${EXTERNALS} || Date.now() > deadline) {
        process.stdout.write(JSON.stringify({ finalized: a.finalizedCount() }));
        return;
      }
      setImmediate(poll);
    })()`, { nodeArgs: ['--expose-gc'] })
  assert.equal(r.status, 0, r.stderr)
  assert.deepEqual(lastJSONLine(r), { finalized: EXTERNALS })
  assert.throws(() => A.makeExternals(-1), TypeError)
})

test('threadsafe function called from a goroutine', async () => {
  const got = []
  await new Promise((resolve) => A.tsfnCollect(TSFN_CALLS, (i) => {
    got.push(i)
    if (got.length === TSFN_CALLS) resolve()
  }))
  assert.deepEqual(got, Array.from({ length: TSFN_CALLS }, (_, i) => i))
  await waitFor(() => A.envCounts().tsfnErrors === 0, 'no threadsafe function errors')

  let calls = 0
  A.tsfnCollect(0, () => { calls++ })
  await new Promise((resolve) => setTimeout(resolve, SETTLE_MS))
  assert.equal(calls, 0)
  assert.equal(A.envCounts().tsfnErrors, 0)
  assert.throws(() => A.tsfnCollect(1, 'not a function'), { code: 'GO_PANIC' })
  assert.throws(() => A.tsfnCollect(1), TypeError)
})

test('async work on the libuv threadpool and Promise', async () => {
  assert.equal(await A.asyncSum(0), 0)
  assert.equal(await A.asyncSum(ASYNC_N), sumTo(ASYNC_N))
  const results = await Promise.all(Array.from({ length: CONCURRENT_JOBS }, (_, i) => A.asyncSum(i)))
  assert.deepEqual(results, Array.from({ length: CONCURRENT_JOBS }, (_, i) => sumTo(i)))
  assert.throws(() => A.asyncSum(-1), TypeError)
})

test('worker_threads: one env per worker and cleanup hook on exit', async () => {
  const before = A.envCounts()
  const src = `
    const { parentPort } = require('node:worker_threads')
    const a = require(${JSON.stringify(addonPath('sysbasic'))})
    a.asyncSum(${WORKER_SUM_N}).then((s) => parentPort.postMessage(a.add(s, 1)))`
  const results = await Promise.all(Array.from({ length: WORKERS }, () => new Promise((resolve, reject) => {
    const w = new Worker(src, { eval: true })
    w.once('message', (m) => { w.terminate().then(() => resolve(m), reject) })
    w.once('error', reject)
  })))
  assert.deepEqual(results, Array(WORKERS).fill(sumTo(WORKER_SUM_N) + 1))
  await waitFor(() => A.envCounts().cleaned >= before.cleaned + WORKERS, 'worker cleanup hooks')
  const after = A.envCounts()
  assert.equal(after.inited, before.inited + WORKERS)
  assert.equal(after.cleaned, before.cleaned + WORKERS)
})

test('two Go addons in the same process', () => {
  const r = nodeEval(`const a = require(${JSON.stringify(addonPath('sysbasic'))});
    let second;
    try {
      const b = require(${JSON.stringify(addonPath('sysbasic-copy'))});
      second = { name: b.name(), add: b.add(2, 3) };
    } catch (e) {
      second = { code: e.code };
    }
    process.stdout.write(JSON.stringify({ first: a.add(1, 2), second }))`)
  const out = lastJSONLine(r)
  assert.equal(out.first, 3)
  if (isDarwinX64) {
    // On darwin/amd64 Go runtimes share the g TLS slot: the second one is rejected.
    assert.deepEqual(out.second, { code: 'ERR_NAPI_GO_FOREIGN_RUNTIME' })
  } else {
    assert.deepEqual(out.second, { name: 'sysbasic-copy', add: 5 })
  }
})

test('zero Go allocations per call, even for functions that can run JS', () => {
  assert.equal(A.allocsPerCall({ k: 1 }, 'hello'), 0)
  assert.throws(() => A.allocsPerCall(), TypeError)
})

test('process exits cleanly on its own with the addon loaded', () => {
  const r = nodeEval(`require(${JSON.stringify(addonPath('sysbasic'))}).add(1, 2)`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.signal, null)
})

test('signals: SIGTERM and SIGINT without listeners terminate the process', { skip: isWin && 'POSIX signals' }, async (t) => {
  const setup = `require(${JSON.stringify(addonPath('sysbasic'))}).add(1, 2)`
  for (const signal of ['SIGTERM', 'SIGINT']) {
    await t.test(signal, async () => {
      const r = await signalChild(setup, signal, { hangMs: SIGNAL_HANG_MS, lifetimeMs: SIGNAL_CHILD_LIFETIME_MS })
      assert.equal(r.sig, signal, `code=${r.code} sig=${r.sig} after ${r.exitMs} ms; stderr: ${r.stderr}`)
    })
  }
})
