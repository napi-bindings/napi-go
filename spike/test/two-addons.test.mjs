// Q5 (risk #1): two separately compiled napi-go addons = two Go runtimes in the
// same Node process.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { Worker } from 'node:worker_threads'
import { load, addonPath } from './helpers.mjs'

// On darwin/amd64 all Go runtimes store g in the same fixed TLS slot 0x30(GS): the second
// runtime finds the first runtime's g on the main thread and uses it as its own (mixed heaps
// and Ms, "fatal error: bad sweepgen in refill"). Go limitation, see README, finding 8.
const DARWIN_X64 = process.platform === 'darwin' && process.arch === 'x64'
const SKIP_IF_SHARED_G_SLOT = DARWIN_X64 && 'darwin/amd64: two Go runtimes share the g TLS slot (Go limitation)'
const CHILD_TIMEOUT_MS = 30_000
const SYSMON_RETAKE_WAIT_MS = 30 // > Go ForcePreemptNS (10 ms): sysmon retakes the P from _Psyscall
const NULL_POINTER_TEXT = '0x0'

test('darwin/amd64: second Go runtime reuses first runtime g (shared TLS slot)', {
  skip: !DARWIN_X64 && 'darwin/amd64 only: elsewhere each runtime has its own slot',
}, (t) => {
  // Run in a separate process: runtime state after this test is unreliable.
  // Write beforeSecond to fd 1 before require('second'): entering the second runtime
  // with the first runtime's g can crash ("fatal error: bad sweepgen in refill") right inside
  // second's goNapiRegisterModule if basic's P is still in _Psyscall, not only on exit.
  const src = `
    const fs = require('node:fs')
    const b = require(${JSON.stringify(addonPath('basic'))})
    const beforeSecond = b.__goTlsSlot()  // basic init: its g remains bound to the main thread
    fs.writeSync(1, beforeSecond + '\\n')
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ${SYSMON_RETAKE_WAIT_MS})
    require(${JSON.stringify(addonPath('second'))})  // second init also enters Go
    const afterSecond = b.__goTlsSlot()
    fs.writeSync(1, afterSecond + '\\n')`
  const r = spawnSync(process.execPath, ['-e', src], { encoding: 'utf8', timeout: CHILD_TIMEOUT_MS })
  assert.equal(r.error, undefined)
  const lines = (r.stdout || '').trim().split('\n').filter(Boolean)
  const [beforeSecond, afterSecond] = lines
  const firstErrLine = (r.stderr || '').split('\n')[0] || '-'
  const outcome = `code=${r.status} sig=${r.signal} stdout=${JSON.stringify(lines)} stderr: ${firstErrLine}`
  t.diagnostic(outcome)
  assert.ok(beforeSecond, `child process did not write beforeSecond: ${outcome}`)
  assert.notEqual(beforeSecond, NULL_POINTER_TEXT, 'basic runtime must have bound a g to the main thread')
  if (afterSecond !== undefined) {
    // If second had its own slot, it would have bound its own g and the slot would have changed.
    assert.equal(afterSecond, beforeSecond)
  } else {
    // Crashed already during second's init from reusing basic's g/mcache (e.g. bad sweepgen in refill).
    assert.match(r.stderr || '', /^fatal error:/m, `expected Go runtime fatal error: ${outcome}`)
  }
})

test('two Go runtimes: loading and synchronous calls', { skip: SKIP_IF_SHARED_G_SLOT }, () => {
  const b = load('basic')
  const s = load('second')
  assert.equal(s.name(), 'second')
  assert.equal(s.mul(6, 7), 42)
  assert.equal(b.add(6, 7), 13)
  // distinct addresses = distinct Go images (and runtimes)
  assert.notEqual(s.goAddr().split(' ')[0], b.info().goAddr)
})

test('two Go runtimes: edge-case inputs, null, and isolated errors per runtime', { skip: SKIP_IF_SHARED_G_SLOT }, async () => {
  const b = load('basic')
  const s = load('second')
  assert.equal(s.mul(Number.MAX_VALUE, 2), Infinity)
  assert.ok(Object.is(s.mul(-0, 1), -0))
  assert.ok(Number.isNaN(s.mul(NaN, 1)))
  assert.throws(() => s.mul(null, 1), /A number was expected/)
  assert.throws(() => s.mul(), /A number was expected/)
  assert.equal(await s.sleep(0), 0)
  // a panic in runtime A does not affect runtime B
  assert.throws(() => b.panicNow(''), { code: 'GO_PANIC', message: 'boom: ' })
  await assert.rejects(b.panicAsync(), { code: 'GO_PANIC' })
  assert.equal(s.mul(3, 3), 9)
  assert.equal(b.echo(''), '')
})

test('two Go runtimes: concurrent async, GC, and CPU in both', { skip: SKIP_IF_SHARED_G_SLOT }, async () => {
  const b = load('basic')
  const s = load('second')
  const jobs = []
  for (let i = 0; i < 20; i++) {
    jobs.push(b.burnCPU(50), s.churn(2000), b.sleep(i % 3), s.sleep(i % 3), b.parallelSum(4, 200_000))
  }
  const res = await Promise.all(jobs)
  assert.equal(res.length, 100)
  b.goGC()
})

test('two Go runtimes: both loaded in concurrent workers', { skip: SKIP_IF_SHARED_G_SLOT }, async () => {
  const src = `
    const { parentPort } = require('node:worker_threads')
    const b = require(${JSON.stringify(addonPath('basic'))})
    const s = require(${JSON.stringify(addonPath('second'))})
    ;(async () => {
      await Promise.all([b.burnCPU(100), s.churn(3000), s.sleep(5), b.sleep(5)])
      parentPort.postMessage(b.add(1, 2) + s.mul(2, 3))
    })()`
  const results = await Promise.all(Array.from({ length: 4 }, () => new Promise((resolve, reject) => {
    const w = new Worker(src, { eval: true })
    w.once('message', (m) => { w.terminate(); resolve(m) })
    w.once('error', reject)
  })))
  assert.deepEqual(results, [9, 9, 9, 9])
})
