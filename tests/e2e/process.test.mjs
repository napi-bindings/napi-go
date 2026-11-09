// End-to-end tests of how a Go addon coexists with the Node.js process: V8's signal handlers
// (WebAssembly traps, SIGINT/SIGTERM), child processes, process exit and worker termination while
// the Go runtime is busy. They use the sysbasic addon (Layer 0) and were moved here from the
// Phase 0 spike.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { execFileSync } from 'node:child_process'
import { constants } from 'node:os'
import { load, addonPath, isWin, nodeEval, lastJSONLine, signalChild } from './helpers.mjs'

const A = load('sysbasic')
const ADDON = JSON.stringify(addonPath('sysbasic'))

const SETTLE_MS = 50
const POLL_LIMIT_MS = 10_000

async function waitFor (predicate, what) {
  const deadline = Date.now() + POLL_LIMIT_MS
  while (!predicate()) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for: ${what}`)
    await new Promise((resolve) => setTimeout(resolve, SETTLE_MS))
  }
}

// (module (memory 1) (func (export "load") (param i32) (result i32) local.get 0 i32.load))
const WASM_LOAD_MODULE = new Uint8Array([
  0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
  0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f,
  0x03, 0x02, 0x01, 0x00,
  0x05, 0x03, 0x01, 0x00, 0x01,
  0x07, 0x08, 0x01, 0x04, 0x6c, 0x6f, 0x61, 0x64, 0x00, 0x00,
  0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b,
])
// Enough traps to overlap several Go GC cycles and preemptions.
const WASM_TRAPS = 500
// 1 MiB: beyond the single 64 KiB page of the module's memory, so every load traps.
const WASM_OUT_OF_BOUNDS = 1 << 20
const BURN_MS = 1500

// V8 implements WebAssembly bounds checks with a SIGSEGV handler (vectored exception handler on
// Windows). The Go runtime installs its own handlers and preempts its goroutines with SIGURG
// (SuspendThread on Windows): every trap must still reach V8 while Go is busy.
test('WebAssembly traps reach V8 while the Go runtime preempts goroutines and collects garbage', async (t) => {
  const { instance } = await WebAssembly.instantiate(WASM_LOAD_MODULE)
  const burners = A.burnCPU(BURN_MS)
  assert.ok(burners >= 1)
  let worstUs = 0
  const t0 = process.hrtime.bigint()
  for (let i = 0; i < WASM_TRAPS; i++) {
    const s = process.hrtime.bigint()
    assert.throws(() => instance.exports.load(WASM_OUT_OF_BOUNDS), WebAssembly.RuntimeError)
    worstUs = Math.max(worstUs, Number(process.hrtime.bigint() - s) / 1000)
    // The instance stays usable after a trap.
    assert.equal(instance.exports.load(0), 0)
  }
  const avgUs = Number(process.hrtime.bigint() - t0) / 1000 / WASM_TRAPS
  // Timings depend on the machine: they are diagnostics, not assertions.
  t.diagnostic(`${WASM_TRAPS} traps with ${burners} burning goroutines, average ${avgUs.toFixed(1)} µs, worst ${worstUs.toFixed(0)} µs`)
  assert.equal(A.add(1, 2), 3)
  await waitFor(() => A.burning() === 0, 'burnCPU goroutines to finish')
})

const CHILD_PROCESSES = 10

test('child processes work with the addon loaded and the Go runtime busy (SIGCHLD)', async () => {
  A.burnCPU(SETTLE_MS)
  for (let i = 0; i < CHILD_PROCESSES; i++) {
    const out = execFileSync(process.execPath, ['-e', 'process.stdout.write("hi")'], { encoding: 'utf8' })
    assert.equal(out, 'hi')
  }
  await waitFor(() => A.burning() === 0, 'burnCPU goroutines to finish')
})

const isLinux = process.platform === 'linux'
const FLOOD_BURN_MS = 2000
const FLOOD_BATCHES = 300
const SIGNALS_PER_BATCH = 20

// libuv installs its handlers (SIGCHLD on the first child process, any signal with a JS listener)
// without SA_ONSTACK: delivered to a Go thread, the signal frame would be written on a goroutine
// stack and overrun it (sys/onstack.c). tgkill sends the signal to every thread, Go threads
// included, which a process-directed signal reaches only by chance; the child process crashes
// within a few batches without the fix.
test('signal handlers installed after load use the alternate stack; signals on Go threads are safe', { skip: !isLinux && 'tgkill and the GOT hook are Linux only' }, () => {
  const { SIGUSR2, SIGCHLD } = constants.signals
  const r = nodeEval(`const a = require(${ADDON});
    const { spawn } = require('node:child_process');
    let handled = 0;
    process.on('SIGUSR2', () => { handled++ });
    // libuv keeps its SIGCHLD handler while a child process is alive.
    const child = spawn(process.execPath, ['-e', 'setInterval(() => {}, ${SETTLE_MS})']);
    const flags = { usr2: a.onStack(${SIGUSR2}), chld: a.onStack(${SIGCHLD}) };
    child.kill();
    a.burnCPU(${FLOOD_BURN_MS});
    (async () => {
      let sent = 0;
      for (let b = 0; b < ${FLOOD_BATCHES}; b++) {
        for (let i = 0; i < ${SIGNALS_PER_BATCH}; i++) sent += a.signalThreads(${SIGUSR2});
        await new Promise((resolve) => setImmediate(resolve));
      }
      while (a.burning() > 0) await new Promise((resolve) => setTimeout(resolve, ${SETTLE_MS}));
      process.stdout.write(JSON.stringify({ ...flags, sent, handled }));
    })()`)
  assert.equal(r.status, 0, r.stderr)
  const out = lastJSONLine(r)
  assert.equal(out.usr2, true, 'the SIGUSR2 handler of process.on lacks SA_ONSTACK')
  assert.equal(out.chld, true, 'the SIGCHLD handler of child_process lacks SA_ONSTACK')
  assert.ok(out.sent > 0, 'no thread was signaled')
  assert.ok(out.handled > 0, 'no SIGUSR2 reached the JS listener')
})

const EXIT_CODE = 7
const EXIT_AFTER_MS = 20
const LONG_BURN_MS = 5000
const PENDING_TSFN_CALLS = 1_000_000

test('process.exit with goroutines running and a threadsafe function in use does not wait for Go', (t) => {
  const t0 = Date.now()
  const r = nodeEval(`const a = require(${ADDON});
    a.burnCPU(${LONG_BURN_MS});
    a.tsfnCollect(${PENDING_TSFN_CALLS}, () => {});
    setTimeout(() => process.exit(${EXIT_CODE}), ${EXIT_AFTER_MS})`)
  const elapsed = Date.now() - t0
  t.diagnostic(`child lived ${elapsed} ms`)
  assert.equal(r.status, EXIT_CODE, r.stderr)
  assert.equal(r.signal, null)
  // Waiting for the goroutines would take about LONG_BURN_MS.
  assert.ok(elapsed < LONG_BURN_MS, `the exit waited for the Go goroutines (${elapsed} ms)`)
})

const GC_ROUNDS = 50
const EXTERNALS_PER_ROUND = 200
const GC_BURN_MS = 5

test('forced V8 GC with Go-owned externals while the Go GC runs', () => {
  const r = nodeEval(`const a = require(${ADDON});
    (async () => {
      for (let i = 0; i < ${GC_ROUNDS}; i++) {
        a.makeExternals(${EXTERNALS_PER_ROUND});
        a.burnCPU(${GC_BURN_MS});
        await new Promise((resolve) => setImmediate(resolve));
        gc();
        a.goGC();
      }
      while (a.burning() > 0) await new Promise((resolve) => setTimeout(resolve, ${SETTLE_MS}));
      gc();
      await new Promise((resolve) => setImmediate(resolve));
      process.stdout.write(JSON.stringify({ finalized: a.finalizedCount() }));
    })()`, { nodeArgs: ['--expose-gc'] })
  assert.equal(r.status, 0, r.stderr)
  assert.ok(lastJSONLine(r).finalized > 0, 'no external was finalized')
})

// ---- signals (POSIX) ----

const SIGNAL_HANG_MS = 5_000
// The child expected to hang is killed sooner: its only purpose is to show the hang.
const EXPECTED_HANG_MS = 2_000
const SIGNAL_CHILD_LIFETIME_MS = 15_000
const SIGINT_JS_EXIT = 42
const SIGTERM_JS_EXIT = 43
const SIGNAL_OPTIONS = { hangMs: SIGNAL_HANG_MS, lifetimeMs: SIGNAL_CHILD_LIFETIME_MS }

const loaded = `const a = require(${ADDON}); a.add(1, 1);`
const signalSetups = [
  { label: 'addon loaded', setup: loaded },
  { label: 'addon with CPU-bound goroutines', setup: `${loaded} a.burnCPU(${SIGNAL_CHILD_LIFETIME_MS});` },
]
const jsHandler = (signal, code) => `process.on('${signal}', () => process.exit(${code}));`
const describe = (r) => `code=${r.code} sig=${r.sig} after ${r.exitMs} ms; stderr: ${r.stderr.trim() || '-'}`

test('SIGINT and SIGTERM reach JS listeners or terminate the process by default', { skip: isWin && 'POSIX signals' }, async (t) => {
  for (const { label, setup } of signalSetups) {
    await t.test(`${label}: SIGINT to a JS listener`, async () => {
      const r = await signalChild(setup + jsHandler('SIGINT', SIGINT_JS_EXIT), 'SIGINT', SIGNAL_OPTIONS)
      assert.equal(r.code, SIGINT_JS_EXIT, describe(r))
    })
    await t.test(`${label}: SIGTERM to a JS listener`, async () => {
      const r = await signalChild(setup + jsHandler('SIGTERM', SIGTERM_JS_EXIT), 'SIGTERM', SIGNAL_OPTIONS)
      assert.equal(r.code, SIGTERM_JS_EXIT, describe(r))
    })
    for (const signal of ['SIGINT', 'SIGTERM']) {
      await t.test(`${label}: ${signal} without listeners terminates the process`, async () => {
        const r = await signalChild(setup, signal, SIGNAL_OPTIONS)
        assert.equal(r.sig, signal, describe(r))
      })
    }
  }
})

// Regression for the macOS hang: the Go runtime (initsig → setsigstack) reinstalls
// node::SignalExit without SA_RESETHAND, so raise() inside the handler re-enters it forever.
// dropResetHand reproduces the loss on any Unix; repairSignalHandlers runs the repair of
// sys/sigfix.c, the same one the module entry point runs.
test('losing SA_RESETHAND on node::SignalExit hangs SIGTERM and the sys repair fixes it', { skip: isWin && 'POSIX signals' }, async (t) => {
  const drop = `${loaded} a.dropResetHand(${constants.signals.SIGTERM});`
  await t.test('without the repair SIGTERM hangs the process (reproduces macOS)', async () => {
    const r = await signalChild(drop, 'SIGTERM', { hangMs: EXPECTED_HANG_MS, lifetimeMs: SIGNAL_CHILD_LIFETIME_MS })
    assert.equal(r.sig, 'SIGKILL', `the loss of SA_RESETHAND should hang the process: ${describe(r)}`)
  })
  await t.test('with the repair SIGTERM terminates the process', async () => {
    const r = await signalChild(`${drop} a.repairSignalHandlers();`, 'SIGTERM', SIGNAL_OPTIONS)
    assert.equal(r.sig, 'SIGTERM', describe(r))
  })
})

// ---- worker termination ----

const TERMINATION_ROUNDS = 5
const PENDING_ASYNC_N = 50_000_000

// Runs in a child process: its stderr must stay empty. While a worker shuts down Node-API can no
// longer run JavaScript, and settling a Promise or throwing must neither crash nor print anything.
test('workers terminated while goroutines, threadsafe functions and async work are pending', () => {
  const r = nodeEval(`
    const { Worker } = require('node:worker_threads')
    const a = require(${ADDON})
    // The addon path reaches the worker through workerData: inside this nested template literal
    // a Windows path would lose its backslashes.
    const src = \`
      const { parentPort, workerData } = require('node:worker_threads')
      const a = require(workerData)
      a.burnCPU(${LONG_BURN_MS})
      a.tsfnCollect(${PENDING_TSFN_CALLS}, () => {})
      a.asyncSum(${PENDING_ASYNC_N})
      parentPort.postMessage('started')\`
    const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms))
    ;(async () => {
      const before = a.envCounts()
      for (let round = 0; round < ${TERMINATION_ROUNDS}; round++) {
        const w = new Worker(src, { eval: true, workerData: ${ADDON} })
        // Both listeners before any await: 'exit' can follow 'message' immediately.
        const exited = new Promise((resolve) => w.once('exit', resolve))
        await new Promise((resolve, reject) => { w.once('message', resolve); w.once('error', reject) })
        await w.terminate()
        await exited
      }
      const deadline = Date.now() + ${POLL_LIMIT_MS}
      while (a.envCounts().cleaned < before.cleaned + ${TERMINATION_ROUNDS} && Date.now() < deadline) await sleep(${SETTLE_MS})
      const after = a.envCounts()
      const sum = await a.asyncSum(4)
      process.stdout.write(JSON.stringify({
        inited: after.inited - before.inited, cleaned: after.cleaned - before.cleaned, add: a.add(1, 1), sum,
      }))
    })()`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stderr, '', 'nothing must be reported on stderr while workers shut down')
  assert.deepEqual(lastJSONLine(r), { inited: TERMINATION_ROUNDS, cleaned: TERMINATION_ROUNDS, add: 2, sum: 6 })
})
