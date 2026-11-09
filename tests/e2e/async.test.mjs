// End-to-end tests of the concurrency APIs of Layer 1 (napi), with the napiasync addon: Promises
// settled from goroutines (Env.Async), the env context, typed threadsafe functions and
// Ref.DeleteLater, including envs destroyed while goroutines still use them.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { load, addonPath, nodeEval, lastJSONLine } from './helpers.mjs'

const A = load('napiasync')
const ADDON = JSON.stringify(addonPath('napiasync'))

const SETTLE_MS = 20
const POLL_LIMIT_MS = 10_000

async function waitFor (predicate, what) {
  const deadline = Date.now() + POLL_LIMIT_MS
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for: ${what}`)
    await new Promise((resolve) => setTimeout(resolve, SETTLE_MS))
  }
}

// ---- Promises settled from goroutines ----

const CONCURRENT_PROMISES = 20_000
const SHORT_SLEEP_MS = 5

test('Async: values, concurrency and resolution order independent of start order', async () => {
  assert.equal(await A.sleepValue(0, 1.5), 1.5)
  assert.equal(await A.resolveUndefined(), undefined)
  // The later-started, shorter work settles first.
  const order = []
  await Promise.all([
    A.sleepValue(SHORT_SLEEP_MS * 10, 1).then((v) => order.push(v)),
    A.sleepValue(0, 2).then((v) => order.push(v)),
  ])
  assert.deepEqual(order, [2, 1])
  const values = await Promise.all(Array.from({ length: CONCURRENT_PROMISES }, (_, i) => A.sleepValue(i % SHORT_SLEEP_MS, i)))
  assert.equal(values.length, CONCURRENT_PROMISES)
  values.forEach((v, i) => assert.equal(v, i))
  assert.equal(A.contextAlive(), true)
})

test('Async: errors from work and resolver reject with kind, code and message', async () => {
  await assert.rejects(A.rejectWith('error', 'E_WORK', 'work failed'), (e) => e instanceof Error && e.code === 'E_WORK' && e.message === 'work failed')
  await assert.rejects(A.rejectWith('type', 'E_TYPE', 'bad type'), (e) => e instanceof TypeError && e.code === 'E_TYPE')
  await assert.rejects(A.rejectWith('range', '', 'out of range'), (e) => e instanceof RangeError && e.message === 'out of range')
  await assert.rejects(A.panicInWork(), (e) => e.code === 'GO_PANIC' && /async work: boom in work/.test(e.message))
  await assert.rejects(A.panicInResolver(), (e) => e.code === 'GO_PANIC' && /async resolver: boom in resolver/.test(e.message))
  // The resolver runs on the JS thread and may call JS; what it throws rejects the Promise as is.
  assert.equal(await A.resolverCalls((n) => n + 1), 42)
  const thrown = { not: 'an Error' }
  await assert.rejects(A.resolverCalls(() => { throw thrown }), (e) => e === thrown)
  assert.throws(() => A.sleepValue('x', 1), TypeError)
})

const LOOP_SLEEP_MS = 300
const EXPECTED_VALUE = 7

test('Async keeps the event loop alive until it settles, and only until then', () => {
  const r = nodeEval(`const a = require(${ADDON});
    a.sleepValue(${LOOP_SLEEP_MS}, ${EXPECTED_VALUE}).then((v) => process.stdout.write(JSON.stringify({ v })))`)
  assert.equal(r.status, 0, r.stderr)
  assert.deepEqual(lastJSONLine(r), { v: EXPECTED_VALUE })
  // With nothing pending the dispatcher does not hold the loop: the process exits on its own.
  const idle = nodeEval(`require(${ADDON}); process.stdout.write('{"loaded":true}')`)
  assert.equal(idle.status, 0, idle.stderr)
  assert.deepEqual(lastJSONLine(idle), { loaded: true })
})

const WORKER_ROUNDS = 5
const WAITERS_PER_WORKER = 3
const LONG_SLEEP_MS = 60_000

test('the env context is canceled when a worker terminates; pending Promises are dropped silently', () => {
  const r = nodeEval(`
    const { Worker } = require('node:worker_threads')
    const a = require(${ADDON})
    const src = \`
      const { parentPort, workerData } = require('node:worker_threads')
      const a = require(workerData)
      for (let i = 0; i < ${WAITERS_PER_WORKER}; i++) a.waitCancel()
      a.sleepValue(${LONG_SLEEP_MS}, 1)
      parentPort.postMessage(a.contextAlive())\`
    ;(async () => {
      const alive = []
      for (let round = 0; round < ${WORKER_ROUNDS}; round++) {
        const w = new Worker(src, { eval: true, workerData: ${ADDON} })
        const exited = new Promise((resolve) => w.once('exit', resolve))
        alive.push(await new Promise((resolve, reject) => { w.once('message', resolve); w.once('error', reject) }))
        await w.terminate()
        await exited
      }
      const deadline = Date.now() + ${POLL_LIMIT_MS}
      while (a.counters().canceled < ${WORKER_ROUNDS * WAITERS_PER_WORKER} && Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, ${SETTLE_MS}))
      }
      process.stdout.write(JSON.stringify({ alive, canceled: a.counters().canceled, mainAlive: a.contextAlive() }))
    })()`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stderr, '', 'nothing must be reported while workers shut down')
  assert.deepEqual(lastJSONLine(r), {
    alive: Array(WORKER_ROUNDS).fill(true), canceled: WORKER_ROUNDS * WAITERS_PER_WORKER, mainAlive: true,
  })
})

// ---- typed threadsafe functions ----

const TSFN_GOROUTINES = 4
const TSFN_CALLS = 2_000
const SMALL_QUEUE = 2

// runTSFN collects the calls made by tsfnRun and checks that each goroutine's calls arrive in
// order, all of them, and that the function is finalized once released by every goroutine.
async function runTSFN (maxQueue, nonBlocking) {
  const finalizedBefore = A.counters().tsfnFinalized
  const next = new Array(TSFN_GOROUTINES).fill(0)
  let total = 0
  await new Promise((resolve, reject) => {
    A.tsfnRun((g, seq) => {
      if (seq !== next[g]) reject(new Error(`goroutine ${g}: seq ${seq}, want ${next[g]}`))
      next[g]++
      if (++total === TSFN_GOROUTINES * TSFN_CALLS) resolve()
    }, TSFN_GOROUTINES, TSFN_CALLS, maxQueue, nonBlocking)
  })
  assert.deepEqual(next, new Array(TSFN_GOROUTINES).fill(TSFN_CALLS))
  await waitFor(() => A.counters().tsfnFinalized === finalizedBefore + 1, 'threadsafe function finalized')
}

test('ThreadsafeFunction: per-goroutine order, unbounded and bounded queues, finalization', async () => {
  await runTSFN(0, false)
  await runTSFN(SMALL_QUEUE, false)
  await runTSFN(SMALL_QUEUE, true)
  assert.equal(A.counters().tsfnOtherErrs, 0)
  assert.throws(() => A.tsfnRun(() => {}, 0, 1, 0, false), RangeError)
})

test('ThreadsafeFunction: queue full, and every method after Release or Abort returns ErrClosing', async () => {
  assert.deepEqual(A.tsfnQueueFull(), ['ok', 'queueFull'])
  assert.deepEqual(A.tsfnAfterClose(false), ['ok', 'closing', 'closing', 'closing'])
  assert.deepEqual(A.tsfnAfterClose(true), ['ok', 'closing', 'closing', 'closing'])
  assert.equal(A.counters().tsfnOtherErrs, 0)
})

test('ThreadsafeFunction: an error or a panic in callJS is an uncaught exception (Node policy)', () => {
  const script = `const a = require(${ADDON});
    const caught = []
    process.on('uncaughtException', (e) => {
      caught.push({ code: e.code, panic: /callJS panic 1/.test(e.message) })
      if (caught.length === 2) process.stdout.write(JSON.stringify(caught))
    })
    a.tsfnThrow('E_CALLJS')`
  // The addon declares Node-API 8: Node reports the exceptions only with this option (or for
  // modules declaring Node-API 10).
  const enforced = nodeEval(script, { nodeArgs: ['--force-node-api-uncaught-exceptions-policy=true'] })
  assert.equal(enforced.status, 0, enforced.stderr)
  assert.deepEqual(lastJSONLine(enforced), [{ code: 'E_CALLJS', panic: false }, { code: 'GO_PANIC', panic: true }])
  // Without it Node drops them with a DEP0168 warning, one per exception, and goes on.
  const legacy = nodeEval(script)
  assert.equal(legacy.status, 0, legacy.stderr)
  assert.equal(legacy.stdout, '')
  assert.equal(legacy.stderr.match(/\[DEP0168\]/g)?.length, 2, legacy.stderr)
})

// A process kept alive only by an open threadsafe function never exits on its own.
const ALIVE_CHECK_MS = 1_500

test('ThreadsafeFunction: an open function keeps the loop alive unless unref\'d', () => {
  const unref = nodeEval(`require(${ADDON}).tsfnIdle(true)`)
  assert.equal(unref.status, 0, unref.stderr)
  assert.throws(() => nodeEval(`require(${ADDON}).tsfnIdle(false)`, { timeout: ALIVE_CHECK_MS }), { code: 'ETIMEDOUT' },
    'the open threadsafe function should keep the process alive')
})

const FLOOD_ROUNDS = 5
const FLOOD_GOROUTINES = 4

// floodWorkers terminates workers whose goroutines call a threadsafe function in a tight loop:
// they must all stop with ErrClosing, with no crash and nothing on stderr.
function floodWorkers (maxQueue, env) {
  return nodeEval(`
    const { Worker } = require('node:worker_threads')
    const a = require(${ADDON})
    const src = \`
      const { parentPort, workerData } = require('node:worker_threads')
      require(workerData).tsfnFlood(${FLOOD_GOROUTINES}, ${maxQueue})
      setTimeout(() => parentPort.postMessage('flooding'), ${SETTLE_MS})\`
    ;(async () => {
      for (let round = 0; round < ${FLOOD_ROUNDS}; round++) {
        const w = new Worker(src, { eval: true, workerData: ${ADDON} })
        const exited = new Promise((resolve) => w.once('exit', resolve))
        await new Promise((resolve, reject) => { w.once('message', resolve); w.once('error', reject) })
        await w.terminate()
        await exited
      }
      const want = ${FLOOD_ROUNDS * FLOOD_GOROUTINES}
      const deadline = Date.now() + ${POLL_LIMIT_MS}
      while ((a.counters().tsfnClosing < want || a.counters().tsfnFinalized < ${FLOOD_ROUNDS}) && Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, ${SETTLE_MS}))
      }
      const c = a.counters()
      process.stdout.write(JSON.stringify({ closing: c.tsfnClosing, finalized: c.tsfnFinalized, other: c.tsfnOtherErrs }))
    })()`, { env })
}

test('ThreadsafeFunction: workers destroyed while goroutines flood it (with and without async preemption)', () => {
  // Without async preemption the use-after-free fixed in sysbasic crashed most runs. With a
  // bounded queue several goroutines wait for room when the env is destroyed, and Node wakes only
  // one waiting thread.
  for (const maxQueue of [0, 1]) {
    for (const env of [undefined, { GODEBUG: 'asyncpreemptoff=1' }]) {
      const r = floodWorkers(maxQueue, env)
      const what = `maxQueue=${maxQueue} env=${JSON.stringify(env)}`
      assert.equal(r.status, 0, `${what}: ${r.stderr}`)
      assert.equal(r.stderr, '', what)
      assert.deepEqual(lastJSONLine(r), { closing: FLOOD_ROUNDS * FLOOD_GOROUTINES, finalized: FLOOD_ROUNDS, other: 0 }, what)
    }
  }
})

// ---- references released from goroutines ----

const TRACKED_OBJECTS = 200

test('Ref.DeleteLater from a goroutine releases the objects; on a destroyed env it is a no-op', () => {
  const r = nodeEval(`const a = require(${ADDON});
    const tick = () => new Promise((resolve) => setImmediate(resolve));
    (async () => {
      a.trackObjects(${TRACKED_OBJECTS})
      gc(); await tick(); gc()
      const kept = a.counters().finalized
      await a.deleteRefsLater()
      const deadline = Date.now() + ${POLL_LIMIT_MS}
      while (a.counters().finalized < ${TRACKED_OBJECTS} && Date.now() < deadline) { gc(); await tick() }
      // References of a worker that has terminated: DeleteLater must not touch its env. The
      // worker's objects are finalized when its env is destroyed.
      const { Worker } = require('node:worker_threads')
      const w = new Worker('require(require("node:worker_threads").workerData).trackObjects(${TRACKED_OBJECTS})', { eval: true, workerData: ${ADDON} })
      await new Promise((resolve) => w.once('exit', resolve))
      await a.deleteRefsLater()
      process.stdout.write(JSON.stringify({ kept, finalized: a.counters().finalized, errors: a.counters().deleteLater }))
    })()`, { nodeArgs: ['--expose-gc'] })
  assert.equal(r.status, 0, r.stderr)
  assert.deepEqual(lastJSONLine(r), { kept: 0, finalized: 2 * TRACKED_OBJECTS, errors: 0 })
})
