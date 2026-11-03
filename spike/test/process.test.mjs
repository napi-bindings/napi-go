// Q6: signal/process coexistence (WebAssembly trap handler, SIGINT/SIGTERM, child_process,
// forced V8 GC) and event loop lifecycle (the TSFN does not keep the process alive).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawn, spawnSync, execFileSync } from 'node:child_process'
import { load, addonPath, isWin } from './helpers.mjs'

// (module (memory 1) (func (export "load") (param i32) (result i32) local.get 0 i32.load))
const wasmBytes = new Uint8Array([
  0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
  0x01, 0x06, 0x01, 0x60, 0x01, 0x7f, 0x01, 0x7f,
  0x03, 0x02, 0x01, 0x00,
  0x05, 0x03, 0x01, 0x00, 0x01,
  0x07, 0x08, 0x01, 0x04, 0x6c, 0x6f, 0x61, 0x64, 0x00, 0x00,
  0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x28, 0x02, 0x00, 0x0b,
])

function nodeEval(code, args = []) {
  const r = spawnSync(process.execPath, [...args, '-e', code], { encoding: 'utf8', timeout: 60_000 })
  if (r.error) throw r.error // spawn failed or timed out
  return r
}

// Verifies correctness (every trap caught, no crashes), not throughput: how many accesses
// fit in a time window depends on the machine (in CI, on 4-vCPU runners with all Ps busy
// in Go, very few fit). Timings are reported in diagnostics.
test('Q6: WebAssembly traps (V8 SIGSEGV/SEH) while the Go runtime performs preemption and GC', { timeout: 120_000 }, async (t) => {
  const b = load('basic')
  const { instance } = await WebAssembly.instantiate(wasmBytes)
  const TRAPS = 500            // enough to overlap with multiple Go GC/preemption cycles
  const OOB_ADDR = 1 << 20     // 1 MiB: beyond the single page (64 KiB) of wasm memory → trap
  const BURN_MS = 1500         // > typical loop duration; earlier test used 500 ms
  const burning = b.burnCPU(BURN_MS) // goroutines on all Ps: preemption (SIGURG/SuspendThread) + Go GC
  let traps = 0
  let worstUs = 0
  const t0 = process.hrtime.bigint()
  for (let i = 0; i < TRAPS; i++) {
    const s = process.hrtime.bigint()
    assert.throws(() => instance.exports.load(OOB_ADDR), WebAssembly.RuntimeError)
    traps++
    worstUs = Math.max(worstUs, Number(process.hrtime.bigint() - s) / 1000)
    assert.equal(instance.exports.load(0), 0) // instance remains usable after the trap
  }
  const avgUs = Number(process.hrtime.bigint() - t0) / 1000 / TRAPS
  t.diagnostic(`${traps} traps, avg ${avgUs.toFixed(1)} µs, worst ${worstUs.toFixed(0)} µs`)
  assert.equal(traps, TRAPS)
  assert.equal(b.add(1, 2), 3) // Go runtime still responds
  assert.equal(await burning, true) // goroutines finish cleanly
})

test('Q6: process exits on its own when idle (TSFN unref)', () => {
  const r = nodeEval(`const b = require(${JSON.stringify(addonPath('basic'))}); b.add(1,2); console.log('ok')`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stdout.trim(), 'ok')
})

test('Q6: process stays alive while Go Promises are pending', () => {
  const r = nodeEval(`const b = require(${JSON.stringify(addonPath('basic'))});
    b.sleep(300).then(r => console.log('resolved', r.ms))`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stdout.trim(), 'resolved 300')
})

test('Q6: process.exit with in-flight goroutines does not crash', (t) => {
  const t0 = Date.now()
  const r = nodeEval(`const b = require(${JSON.stringify(addonPath('basic'))});
    for (let i = 0; i < 1000; i++) b.sleep(5);
    b.burnCPU(1000);
    setTimeout(() => process.exit(7), 20)`)
  // if exit waited for goroutines, the child would take roughly burnCPU's duration (1000 ms)
  t.diagnostic(`child process duration ${Date.now() - t0} ms`)
  assert.equal(r.status, 7, r.stderr)
})

test('Q6: forced V8 GC with Go-created objects and Go GC pressure', () => {
  const r = nodeEval(`const b = require(${JSON.stringify(addonPath('basic'))});
    (async () => {
      for (let i = 0; i < 50; i++) {
        const objs = Array.from({ length: 200 }, () => b.makeObject(50));
        await Promise.all([b.burnCPU(5), ...Array.from({ length: 50 }, () => b.sleep(0))]);
        global.gc(); b.goGC();
      }
      console.log('ok')
    })()`, ['--expose-gc'])
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stdout.trim(), 'ok')
})

test('Q6: child_process (SIGCHLD) works with the addon loaded', () => {
  const b = load('basic')
  b.burnCPU(200)
  for (let i = 0; i < 10; i++) {
    const out = execFileSync(process.execPath, ['-e', 'process.stdout.write("hi")'], { encoding: 'utf8' })
    assert.equal(out, 'hi')
  }
})

// POSIX signals in the child process. The child has a bounded lifetime (CHILD_LIFETIME_MS): if the
// signal does not reach the JS handler, it exits with LOST_SIGNAL_EXIT instead of hanging. If it is
// still alive HANG_AFTER_MS after the signal, the parent captures thread stacks (`sample` on macOS,
// /proc on Linux) and kills it with SIGKILL. Variants isolate the cause:
//  - Node only / addon loaded / addon + CPU-bound goroutines / addon + JS handler for SIGTERM;
//  - the child prints __sigInfo (diag.c, pure C): who handles SIGTERM/SIGINT and whether they are
//    blocked, before and after a call into Go (since Go 1.21 the M stays bound to the JS thread
//    after the callback).
// Observed on macOS in CI: SIGTERM with Node's handler (node::SignalExit) fails to terminate the
// child when the Go runtime is loaded; SIGINT with a JS handler works.
const CHILD_LIFETIME_MS = 15_000
const HANG_AFTER_MS = 5_000
const LOST_SIGNAL_EXIT = 99
const SIGINT_EXIT = 42
const SIGTERM_JS_EXIT = 43
const DIAG_PREFIX = 'diag:'
const NO_DIAG = '-'

function captureStacks(pid) {
  const run = (cmd, args) => {
    const r = spawnSync(cmd, args, { encoding: 'utf8', timeout: 20_000 })
    return r.error ? `${cmd}: ${r.error.message}` : (r.stdout || r.stderr || '').trim()
  }
  if (process.platform === 'darwin') {
    return [run('ps', ['-M', '-p', String(pid)]), run('sample', [String(pid), '1', '-mayDie'])].join('\n')
  }
  if (process.platform === 'linux') {
    return run('sh', ['-c', `grep -E '^(State|SigQ|SigPnd|ShdPnd|SigBlk|SigIgn|SigCgt)' /proc/${pid}/status; ` +
      `for t in /proc/${pid}/task/*; do echo "$t $(cat $t/comm) $(cat $t/wchan 2>/dev/null) $(grep -E '^(State|SigBlk|SigPnd)' $t/status | tr '\\n' ' ')"; done`])
  }
  return ''
}

function runSignalChild(setup, signal, { jsSigint = true, captureOnHang = true } = {}) {
  const sigintListener = jsSigint
    ? `process.on('SIGINT', () => { console.log('got SIGINT'); process.exit(${SIGINT_EXIT}) });`
    : '' // SIGINT remains on node::SignalExit (default termination)
  const script = `const os = require('node:os');
    ${setup}
    if (typeof b !== 'undefined') {
      const S = os.constants.signals;
      console.log('${DIAG_PREFIX}' + JSON.stringify({ SIGTERM: b.__sigInfo(S.SIGTERM), SIGINT: b.__sigInfo(S.SIGINT) }));
    }
    ${sigintListener}
    setTimeout(() => { console.log('signal not received'); process.exit(${LOST_SIGNAL_EXIT}) }, ${CHILD_LIFETIME_MS});
    console.log('ready')`
  return new Promise((resolve, reject) => {
    const c = spawn(process.execPath, ['-e', script], { stdio: ['ignore', 'pipe', 'pipe'] })
    let out = ''
    let err = ''
    let stacks = ''
    let signaledAt = 0
    let hangTimer = null
    c.once('error', (e) => { clearTimeout(hangTimer); reject(e) })
    c.stderr.on('data', (d) => { err += d })
    c.stdout.on('data', (d) => {
      out += d
      if (!signaledAt && out.includes('ready')) {
        signaledAt = Date.now()
        c.kill(signal)
        hangTimer = setTimeout(() => {
          if (captureOnHang) stacks = captureStacks(c.pid)
          c.kill('SIGKILL')
        }, HANG_AFTER_MS)
      }
    })
    c.on('exit', (code, sig) => {
      clearTimeout(hangTimer)
      const diagLine = out.split('\n').find((l) => l.startsWith(DIAG_PREFIX))
      resolve({
        code, sig, out, err, stacks,
        diag: diagLine ? diagLine.slice(DIAG_PREFIX.length) : NO_DIAG,
        exitMs: signaledAt ? Date.now() - signaledAt : -1,
      })
    })
  })
}

function report(st, r) {
  st.diagnostic(`code=${r.code} sig=${r.sig} exited ${r.exitMs} ms after signal; stderr: ${r.err.trim() || '-'}`)
  st.diagnostic(`child signals: ${r.diag}`)
  if (r.stacks) st.diagnostic(`child hung, thread stacks:\n${r.stacks.split('\n').slice(0, 150).join('\n')}`)
}

const addonReq = `const b = require(${JSON.stringify(addonPath('basic'))});`
const signalVariants = [
  { label: 'Node only', setup: '' },
  { label: 'addon loaded', setup: `${addonReq} b.add(1, 1);` },
  { label: 'addon + CPU-bound goroutines', setup: `${addonReq} b.burnCPU(${CHILD_LIFETIME_MS});` },
]

test('Q6: SIGINT handled by process.on and default SIGTERM', { skip: isWin && 'POSIX signals', timeout: 120_000 }, async (t) => {
  for (const { label, setup } of signalVariants) {
    await t.test(`${label}: SIGINT → JS handler`, async (st) => {
      const r = await runSignalChild(setup, 'SIGINT')
      report(st, r)
      assert.notEqual(r.code, LOST_SIGNAL_EXIT, 'SIGINT not delivered to JS handler')
      assert.notEqual(r.sig, 'SIGKILL', 'child hung after signal: killed with SIGKILL')
      assert.equal(r.code, SIGINT_EXIT)
      assert.match(r.out, /got SIGINT/)
    })
    await t.test(`${label}: SIGINT → default termination (without JS handler)`, async (st) => {
      const r = await runSignalChild(setup, 'SIGINT', { jsSigint: false })
      report(st, r)
      assert.notEqual(r.sig, 'SIGKILL', 'child hung after signal: killed with SIGKILL')
      assert.equal(r.sig, 'SIGINT')
    })
    await t.test(`${label}: SIGTERM → default termination`, async (st) => {
      const r = await runSignalChild(setup, 'SIGTERM')
      report(st, r)
      assert.notEqual(r.sig, 'SIGKILL', 'child hung after signal: killed with SIGKILL')
      assert.equal(r.sig, 'SIGTERM')
    })
  }
  await t.test('addon loaded: SIGTERM → JS handler', async (st) => {
    const setup = `${addonReq} b.add(1, 1); process.on('SIGTERM', () => { console.log('got SIGTERM'); process.exit(${SIGTERM_JS_EXIT}) });`
    const r = await runSignalChild(setup, 'SIGTERM')
    report(st, r)
    assert.notEqual(r.sig, 'SIGKILL', 'child hung after signal: killed with SIGKILL')
    assert.equal(r.code, SIGTERM_JS_EXIT)
  })
})

// Regression test for the hang observed on macOS: the Go runtime (initsig → setsigstack) reinstalls
// node::SignalExit dropping SA_RESETHAND, and raise() inside the handler re-invokes it endlessly.
// __sigEmulateDarwinLoss reproduces the loss on any Unix; __sigRepair runs the fix from
// napi/sigfix.c (the same call made at module startup).
test('Q6: SA_RESETHAND lost on node::SignalExit (macOS) and napi-go fix', { skip: isWin && 'POSIX signals', timeout: 60_000 }, async (t) => {
  await t.test('without fix, SIGTERM hangs the process (reproduces macOS)', async (st) => {
    const setup = `${addonReq} b.__sigEmulateDarwinLoss(require('node:os').constants.signals.SIGTERM);`
    const r = await runSignalChild(setup, 'SIGTERM', { captureOnHang: false })
    report(st, r)
    assert.equal(r.sig, 'SIGKILL', 'losing SA_RESETHAND should hang the process')
  })
  await t.test('with fix, SIGTERM terminates the process', async (st) => {
    const setup = `${addonReq} b.__sigEmulateDarwinLoss(require('node:os').constants.signals.SIGTERM); b.__sigRepair();`
    const r = await runSignalChild(setup, 'SIGTERM')
    report(st, r)
    assert.equal(r.sig, 'SIGTERM')
    // Do not check info.resethand: on macOS sigaction() never reports SA_RESETHAND on
    // read-back, even when active (see experiments/sigflags). What matters is the behavior.
    assert.notEqual(r.diag, NO_DIAG, 'child did not print signal diagnostics')
    assert.equal(JSON.parse(r.diag).SIGTERM.repairedByNapiGo, true)
  })
})
