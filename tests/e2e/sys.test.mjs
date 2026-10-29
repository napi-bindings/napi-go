// Test end-to-end del Layer 0 (pacchetto sys) attraverso l'addon tests/e2e/addons/sysbasic.
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
const STACK_STRING_MAX = 64 // stackStringMax nell'addon
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

// 0 + 1 + ... + (n-1); per n = 0 la formula darebbe -0.
const sumTo = (n) => (n > 0 ? (n * (n - 1)) / 2 : 0)

async function waitFor (predicate, what) {
  const deadline = Date.now() + POLL_LIMIT_MS
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error(`timeout in attesa di: ${what}`)
    await new Promise((resolve) => setTimeout(resolve, SETTLE_MS))
  }
}

test('il modulo si carica e dichiara Node-API 8 per default', () => {
  assert.equal(A.name(), 'sysbasic')
  assert.equal(A.declaredVersion(), DEFAULT_DECLARED_VERSION)
  assert.equal(A.moduleAPIVersion(), DEFAULT_DECLARED_VERSION)
})

test('build tag napi_go_v10: il modulo dichiara Node-API 10', { skip: hostNapiVersion < V10 && 'Node senza Node-API 10' }, () => {
  // In un processo separato: due addon Go nello stesso processo non sono ammessi su macOS x64.
  const r = nodeEval(`const a = require(${JSON.stringify(addonPath('sysbasic-v10'))});
    process.stdout.write(JSON.stringify({ name: a.name(), declared: a.declaredVersion(), module: a.moduleAPIVersion() }))`)
  assert.deepEqual(lastJSONLine(r), { name: 'sysbasic-v10', declared: V10, module: V10 })
})

test('funzioni risolte a runtime e funzioni facoltative', () => {
  assert.equal(A.available('napi_create_int32'), true)
  assert.equal(A.available('node_api_symbol_for'), hostNapiVersion >= V9)
  assert.equal(A.available('node_api_create_property_key_utf8'), hostNapiVersion >= V10)
  assert.throws(() => A.available('napi_inesistente'), TypeError)
  assert.throws(() => A.available(''), TypeError)
  assert.throws(() => A.available(null), TypeError)
  assert.throws(() => A.available(), TypeError)
})

test('numeri: valori limite e tipi errati', () => {
  assert.equal(A.add(1.5, 2.5), 4)
  assert.ok(Object.is(A.add(-0, -0), -0))
  assert.ok(Number.isNaN(A.add(NaN, 1)))
  assert.equal(A.add(Number.MAX_VALUE, Number.MAX_VALUE), Infinity)
  assert.equal(A.add(Number.MIN_VALUE, 0), Number.MIN_VALUE)
  assert.throws(() => A.add(1), TypeError)
  assert.throws(() => A.add(null, 1), TypeError)
  assert.throws(() => A.add('1', 2), TypeError)
})

test('stringhe: vuota, UTF-8, confine del buffer sullo stack, lunga', () => {
  assert.equal(A.echo(''), '')
  assert.equal(A.echo('ascii'), 'ascii')
  assert.equal(A.echo('€😀 àèìòù'), '€😀 àèìòù')
  for (const n of [STACK_STRING_MAX - 1, STACK_STRING_MAX, STACK_STRING_MAX + 1]) {
    const s = 'x'.repeat(n)
    assert.equal(A.echo(s), s, `lunghezza ${n}`)
  }
  const long = 'é'.repeat(LONG_STRING_LENGTH)
  assert.equal(A.echo(long), long)
  assert.throws(() => A.echo(null), TypeError)
  assert.throws(() => A.echo(1), TypeError)
  assert.throws(() => A.echo(), TypeError)
})

test('argomenti: nessuno, confine del buffer inline, molti', () => {
  assert.equal(A.countArgs(), 0)
  for (const n of [INLINE_ARGS - 1, INLINE_ARGS, INLINE_ARGS + 1, MANY_ARGS]) {
    assert.equal(A.countArgs(...Array(n).fill(0)), n, `${n} argomenti`)
  }
  const values = Array.from({ length: MANY_ARGS }, (_, i) => i)
  assert.equal(A.sumArgs(...values), sumTo(MANY_ARGS))
  assert.equal(A.sumArgs(), 0)
  assert.throws(() => A.sumArgs(1, 'x'), TypeError)
  const obj = { f: A.thisValue }
  assert.equal(obj.f(), obj)
})

test('errori: panic, codici, informazioni estese', () => {
  assert.throws(() => A.panicNow('x'), { code: 'GO_PANIC', message: /boom: x/ })
  assert.throws(() => A.panicNow(), { code: 'GO_PANIC', message: /boom: $/ })
  assert.equal(A.add(1, 2), 3, 'il modulo funziona dopo un panic')
  assert.throws(() => A.throwCoded('E_TEST', 'messaggio'), { code: 'E_TEST', message: 'messaggio' })
  assert.throws(() => A.throwCoded('E_TEST'), TypeError)
  assert.deepEqual(A.lastError(), {
    status: 'napi_number_expected',
    infoStatus: 'napi_number_expected',
    message: 'A number was expected',
  })
})

test('rientranza JS → Go → JS → Go ed eccezioni JS attraverso Go', () => {
  assert.equal(A.callMe((x) => x * 2, 21), 42)
  assert.equal(A.callMe(() => A.callMe(() => A.add(1, 2))), 3)
  assert.equal(A.callMe(() => undefined), undefined)
  assert.throws(() => A.callMe(() => { throw new RangeError('da JS') }), { name: 'RangeError', message: 'da JS' })
  assert.throws(() => A.callMe(), TypeError)
})

test('type tag: oggetti marcati, doppia marcatura, primitivi, null', () => {
  const obj = {}
  assert.equal(A.tagObject(obj, 1, 2), 'napi_ok')
  assert.equal(A.tagObject(obj, 1, 2), 'napi_invalid_arg')
  assert.equal(A.checkTag(obj, 1, 2), true)
  assert.equal(A.checkTag(obj, 1, 3), false)
  assert.equal(A.checkTag({}, 1, 2), false)
  // Node converte i primitivi in oggetto (ToObject): il tag finisce su un wrapper temporaneo.
  assert.equal(A.tagObject(1, 0, 0), 'napi_ok')
  assert.throws(() => A.tagObject(null, 0, 0), TypeError)
  assert.throws(() => A.tagObject(undefined, 0, 0), TypeError)
  assert.throws(() => A.tagObject(obj), TypeError)
})

test('PropertyDescriptor in memoria Go con runtime.Pinner', () => {
  const obj = A.defineProps({})
  assert.equal(obj.hello(), 'hello')
  assert.deepEqual(Object.getOwnPropertyDescriptor(obj, 'answer'), {
    value: 42, writable: false, enumerable: true, configurable: false,
  })
  assert.equal(Object.getOwnPropertyDescriptor(obj, 'hello').enumerable, false)
  assert.throws(() => A.defineProps(), TypeError)
})

test('versione di Node e funzioni Node-API 9/10', () => {
  const v = A.nodeVersion()
  // napi_node_version espone solo major.minor.patch: su una prerelease (es. "27.0.0-alpha.0.0.0")
  // il suffisso non compare.
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

test('finalizer: External raccolti dal GC di V8', () => {
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

test('threadsafe function chiamata da una goroutine', async () => {
  const got = []
  await new Promise((resolve) => A.tsfnCollect(TSFN_CALLS, (i) => {
    got.push(i)
    if (got.length === TSFN_CALLS) resolve()
  }))
  assert.deepEqual(got, Array.from({ length: TSFN_CALLS }, (_, i) => i))
  await waitFor(() => A.envCounts().tsfnErrors === 0, 'nessun errore della threadsafe function')

  let calls = 0
  A.tsfnCollect(0, () => { calls++ })
  await new Promise((resolve) => setTimeout(resolve, SETTLE_MS))
  assert.equal(calls, 0)
  assert.equal(A.envCounts().tsfnErrors, 0)
  assert.throws(() => A.tsfnCollect(1, 'non una funzione'), { code: 'GO_PANIC' })
  assert.throws(() => A.tsfnCollect(1), TypeError)
})

test('async work sul pool di libuv e Promise', async () => {
  assert.equal(await A.asyncSum(0), 0)
  assert.equal(await A.asyncSum(ASYNC_N), sumTo(ASYNC_N))
  const results = await Promise.all(Array.from({ length: CONCURRENT_JOBS }, (_, i) => A.asyncSum(i)))
  assert.deepEqual(results, Array.from({ length: CONCURRENT_JOBS }, (_, i) => sumTo(i)))
  assert.throws(() => A.asyncSum(-1), TypeError)
})

test('worker_threads: un env per worker e cleanup hook alla chiusura', async () => {
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
  await waitFor(() => A.envCounts().cleaned >= before.cleaned + WORKERS, 'cleanup hook dei worker')
  const after = A.envCounts()
  assert.equal(after.inited, before.inited + WORKERS)
  assert.equal(after.cleaned, before.cleaned + WORKERS)
})

test('due addon Go nello stesso processo', () => {
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
    // Su darwin/amd64 i runtime Go condividono lo slot TLS di g: il secondo viene rifiutato.
    assert.deepEqual(out.second, { code: 'ERR_NAPI_GO_FOREIGN_RUNTIME' })
  } else {
    assert.deepEqual(out.second, { name: 'sysbasic-copy', add: 5 })
  }
})

test('nessuna allocazione Go per chiamata, anche per funzioni che possono eseguire JS', () => {
  assert.equal(A.allocsPerCall({ k: 1 }, 'hello'), 0)
  assert.throws(() => A.allocsPerCall(), TypeError)
})

test('il processo termina da solo con l\'addon caricato', () => {
  const r = nodeEval(`require(${JSON.stringify(addonPath('sysbasic'))}).add(1, 2)`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.signal, null)
})

test('segnali: SIGTERM e SIGINT senza listener terminano il processo', { skip: isWin && 'segnali POSIX' }, async (t) => {
  const setup = `require(${JSON.stringify(addonPath('sysbasic'))}).add(1, 2)`
  for (const signal of ['SIGTERM', 'SIGINT']) {
    await t.test(signal, async () => {
      const r = await signalChild(setup, signal, { hangMs: SIGNAL_HANG_MS, lifetimeMs: SIGNAL_CHILD_LIFETIME_MS })
      assert.equal(r.sig, signal, `code=${r.code} sig=${r.sig} dopo ${r.exitMs} ms; stderr: ${r.stderr}`)
    })
  }
})
