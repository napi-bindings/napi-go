// Q6: convivenza segnali/processo (trap handler WebAssembly, SIGINT/SIGTERM, child_process,
// GC di V8 forzato) e ciclo di vita dell'event loop (la TSFN non tiene vivo il processo).
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
  if (r.error) throw r.error // spawn fallito o timeout
  return r
}

// Verifica la correttezza (ogni trap intercettata, nessun crash), non il throughput: quanti
// accessi entrano in un intervallo di tempo dipende dalla macchina (in CI, runner da 4 vCPU
// con tutte le P occupate da Go, ne entravano pochissimi). I tempi vanno nella diagnostica.
test('Q6: trap WebAssembly (SIGSEGV/SEH di V8) mentre il runtime Go fa preemption e GC', { timeout: 120_000 }, async (t) => {
  const b = load('basic')
  const { instance } = await WebAssembly.instantiate(wasmBytes)
  const TRAPS = 500            // abbastanza da sovrapporsi a più cicli di GC/preemption Go
  const OOB_ADDR = 1 << 20     // 1 MiB: oltre l'unica pagina (64 KiB) di memoria wasm → trap
  const BURN_MS = 1500         // > durata tipica del ciclo; il vecchio test usava 500 ms
  const burning = b.burnCPU(BURN_MS) // goroutine su tutte le P: preemption (SIGURG/SuspendThread) + GC Go
  let traps = 0
  let worstUs = 0
  const t0 = process.hrtime.bigint()
  for (let i = 0; i < TRAPS; i++) {
    const s = process.hrtime.bigint()
    assert.throws(() => instance.exports.load(OOB_ADDR), WebAssembly.RuntimeError)
    traps++
    worstUs = Math.max(worstUs, Number(process.hrtime.bigint() - s) / 1000)
    assert.equal(instance.exports.load(0), 0) // l'istanza resta utilizzabile dopo la trap
  }
  const avgUs = Number(process.hrtime.bigint() - t0) / 1000 / TRAPS
  t.diagnostic(`${traps} trap, media ${avgUs.toFixed(1)} µs, peggiore ${worstUs.toFixed(0)} µs`)
  assert.equal(traps, TRAPS)
  assert.equal(b.add(1, 2), 3) // il runtime Go risponde ancora
  assert.equal(await burning, true) // le goroutine terminano regolarmente
})

test('Q6: il processo termina da solo quando non c\'è lavoro (TSFN unref)', () => {
  const r = nodeEval(`const b = require(${JSON.stringify(addonPath('basic'))}); b.add(1,2); console.log('ok')`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stdout.trim(), 'ok')
})

test('Q6: il processo resta vivo finché ci sono Promise Go pendenti', () => {
  const r = nodeEval(`const b = require(${JSON.stringify(addonPath('basic'))});
    b.sleep(300).then(r => console.log('resolved', r.ms))`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.stdout.trim(), 'resolved 300')
})

test('Q6: process.exit con goroutine in volo non crasha', (t) => {
  const t0 = Date.now()
  const r = nodeEval(`const b = require(${JSON.stringify(addonPath('basic'))});
    for (let i = 0; i < 1000; i++) b.sleep(5);
    b.burnCPU(1000);
    setTimeout(() => process.exit(7), 20)`)
  // se l'uscita aspettasse le goroutine, il figlio durerebbe circa quanto burnCPU (1000 ms)
  t.diagnostic(`durata del processo figlio ${Date.now() - t0} ms`)
  assert.equal(r.status, 7, r.stderr)
})

test('Q6: GC di V8 forzato con oggetti creati da Go e pressione GC Go', () => {
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

test('Q6: child_process (SIGCHLD) funziona con l\'addon caricato', () => {
  const b = load('basic')
  b.burnCPU(200)
  for (let i = 0; i < 10; i++) {
    const out = execFileSync(process.execPath, ['-e', 'process.stdout.write("hi")'], { encoding: 'utf8' })
    assert.equal(out, 'hi')
  }
})

// Segnali POSIX nel processo figlio. Il figlio ha una vita limitata (CHILD_LIFETIME_MS): se il
// segnale non arriva all'handler JS esce con LOST_SIGNAL_EXIT invece di restare appeso; il padre
// lo uccide comunque con SIGKILL dopo KILL_AFTER_MS. Le varianti isolano la causa di un
// eventuale segnale perso (osservato su macOS in CI): Node da solo, addon caricato, addon con
// goroutine CPU-bound su tutte le P.
const CHILD_LIFETIME_MS = 15_000
const KILL_AFTER_MS = 30_000
const LOST_SIGNAL_EXIT = 99
const SIGINT_EXIT = 42

function runSignalChild(setup, signal) {
  const script = `${setup}
    process.on('SIGINT', () => { console.log('got SIGINT'); process.exit(${SIGINT_EXIT}) });
    setTimeout(() => { console.log('segnale non ricevuto'); process.exit(${LOST_SIGNAL_EXIT}) }, ${CHILD_LIFETIME_MS});
    console.log('ready')`
  return new Promise((resolve, reject) => {
    const t0 = Date.now()
    const c = spawn(process.execPath, ['-e', script], { stdio: ['ignore', 'pipe', 'pipe'] })
    const killer = setTimeout(() => c.kill('SIGKILL'), KILL_AFTER_MS)
    let out = ''
    let err = ''
    let signaledAt = 0
    c.once('error', (e) => { clearTimeout(killer); reject(e) })
    c.stderr.on('data', (d) => { err += d })
    c.stdout.on('data', (d) => {
      out += d
      if (!signaledAt && out.includes('ready')) {
        signaledAt = Date.now()
        c.kill(signal)
      }
    })
    c.on('exit', (code, sig) => {
      clearTimeout(killer)
      resolve({ code, sig, out, err, exitMs: signaledAt ? Date.now() - signaledAt : -1, totalMs: Date.now() - t0 })
    })
  })
}

const addonReq = `const b = require(${JSON.stringify(addonPath('basic'))});`
const signalVariants = [
  ['solo Node', ''],
  ['addon caricato', `${addonReq} b.add(1, 1);`],
  ['addon + goroutine CPU-bound', `${addonReq} b.burnCPU(${CHILD_LIFETIME_MS});`],
]

test('Q6: SIGINT gestito da process.on e SIGTERM di default', { skip: isWin && 'segnali POSIX', timeout: 4 * KILL_AFTER_MS }, async (t) => {
  for (const [label, setup] of signalVariants) {
    await t.test(`${label}: SIGINT → handler JS`, async (st) => {
      const r = await runSignalChild(setup, 'SIGINT')
      st.diagnostic(`code=${r.code} sig=${r.sig} uscita dopo il segnale ${r.exitMs} ms; stderr: ${r.err.trim() || '-'}`)
      assert.notEqual(r.code, LOST_SIGNAL_EXIT, 'SIGINT non consegnato all\'handler JS')
      assert.notEqual(r.sig, 'SIGKILL', 'figlio appeso: ucciso con SIGKILL')
      assert.equal(r.code, SIGINT_EXIT)
      assert.match(r.out, /got SIGINT/)
    })
    await t.test(`${label}: SIGTERM → terminazione di default`, async (st) => {
      const r = await runSignalChild(setup, 'SIGTERM')
      st.diagnostic(`code=${r.code} sig=${r.sig} uscita dopo il segnale ${r.exitMs} ms`)
      assert.equal(r.sig, 'SIGTERM')
    })
  }
})
