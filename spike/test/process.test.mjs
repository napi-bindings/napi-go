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
// segnale non arriva all'handler JS esce con LOST_SIGNAL_EXIT invece di restare appeso. Se dopo
// HANG_AFTER_MS dal segnale è ancora vivo, il padre ne cattura gli stack dei thread (`sample` su
// macOS, /proc su Linux) e lo uccide con SIGKILL. Le varianti isolano la causa:
//  - solo Node / addon caricato / addon + goroutine CPU-bound / addon + handler JS per SIGTERM;
//  - il figlio stampa __sigInfo (diag.c, C puro): chi gestisce SIGTERM/SIGINT e se sono bloccati,
//    prima e dopo una chiamata in Go (da Go 1.21 l'M resta legato al thread JS dopo la callback).
// Osservato su macOS in CI: SIGTERM con l'handler di Node (node::SignalExit) non termina il figlio
// quando il runtime Go è caricato; SIGINT con handler JS funziona.
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
    : '' // SIGINT resta a node::SignalExit (terminazione di default)
  const script = `const os = require('node:os');
    ${setup}
    if (typeof b !== 'undefined') {
      const S = os.constants.signals;
      console.log('${DIAG_PREFIX}' + JSON.stringify({ SIGTERM: b.__sigInfo(S.SIGTERM), SIGINT: b.__sigInfo(S.SIGINT) }));
    }
    ${sigintListener}
    setTimeout(() => { console.log('segnale non ricevuto'); process.exit(${LOST_SIGNAL_EXIT}) }, ${CHILD_LIFETIME_MS});
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
  st.diagnostic(`code=${r.code} sig=${r.sig} uscita dopo il segnale ${r.exitMs} ms; stderr: ${r.err.trim() || '-'}`)
  st.diagnostic(`segnali nel figlio: ${r.diag}`)
  if (r.stacks) st.diagnostic(`figlio bloccato, stack dei thread:\n${r.stacks.split('\n').slice(0, 150).join('\n')}`)
}

const addonReq = `const b = require(${JSON.stringify(addonPath('basic'))});`
const signalVariants = [
  { label: 'solo Node', setup: '' },
  { label: 'addon caricato', setup: `${addonReq} b.add(1, 1);` },
  { label: 'addon + goroutine CPU-bound', setup: `${addonReq} b.burnCPU(${CHILD_LIFETIME_MS});` },
]

test('Q6: SIGINT gestito da process.on e SIGTERM di default', { skip: isWin && 'segnali POSIX', timeout: 120_000 }, async (t) => {
  for (const { label, setup } of signalVariants) {
    await t.test(`${label}: SIGINT → handler JS`, async (st) => {
      const r = await runSignalChild(setup, 'SIGINT')
      report(st, r)
      assert.notEqual(r.code, LOST_SIGNAL_EXIT, 'SIGINT non consegnato all\'handler JS')
      assert.notEqual(r.sig, 'SIGKILL', 'figlio bloccato dopo il segnale: ucciso con SIGKILL')
      assert.equal(r.code, SIGINT_EXIT)
      assert.match(r.out, /got SIGINT/)
    })
    await t.test(`${label}: SIGINT → terminazione di default (senza handler JS)`, async (st) => {
      const r = await runSignalChild(setup, 'SIGINT', { jsSigint: false })
      report(st, r)
      assert.notEqual(r.sig, 'SIGKILL', 'figlio bloccato dopo il segnale: ucciso con SIGKILL')
      assert.equal(r.sig, 'SIGINT')
    })
    await t.test(`${label}: SIGTERM → terminazione di default`, async (st) => {
      const r = await runSignalChild(setup, 'SIGTERM')
      report(st, r)
      assert.notEqual(r.sig, 'SIGKILL', 'figlio bloccato dopo il segnale: ucciso con SIGKILL')
      assert.equal(r.sig, 'SIGTERM')
    })
  }
  await t.test('addon caricato: SIGTERM → handler JS', async (st) => {
    const setup = `${addonReq} b.add(1, 1); process.on('SIGTERM', () => { console.log('got SIGTERM'); process.exit(${SIGTERM_JS_EXIT}) });`
    const r = await runSignalChild(setup, 'SIGTERM')
    report(st, r)
    assert.notEqual(r.sig, 'SIGKILL', 'figlio bloccato dopo il segnale: ucciso con SIGKILL')
    assert.equal(r.code, SIGTERM_JS_EXIT)
  })
})

// Regressione del blocco osservato su macOS: il runtime Go (initsig → setsigstack) reinstalla
// node::SignalExit perdendo SA_RESETHAND, e raise() dentro l'handler lo richiama all'infinito.
// __sigEmulateDarwinLoss riproduce la perdita su qualsiasi Unix; __sigRepair esegue la correzione
// di napi/sigfix.c (la stessa chiamata all'avvio del modulo).
test('Q6: SA_RESETHAND perso su node::SignalExit (macOS) e correzione di napi-go', { skip: isWin && 'segnali POSIX', timeout: 60_000 }, async (t) => {
  await t.test('senza correzione SIGTERM blocca il processo (riproduce macOS)', async (st) => {
    const setup = `${addonReq} b.__sigEmulateDarwinLoss(require('node:os').constants.signals.SIGTERM);`
    const r = await runSignalChild(setup, 'SIGTERM', { captureOnHang: false })
    report(st, r)
    assert.equal(r.sig, 'SIGKILL', 'la perdita di SA_RESETHAND dovrebbe bloccare il processo')
  })
  await t.test('con la correzione SIGTERM termina il processo', async (st) => {
    const setup = `${addonReq} b.__sigEmulateDarwinLoss(require('node:os').constants.signals.SIGTERM); b.__sigRepair();`
    const r = await runSignalChild(setup, 'SIGTERM')
    report(st, r)
    assert.equal(r.sig, 'SIGTERM')
    // Non si verifica info.resethand: su macOS sigaction() non riporta mai SA_RESETHAND in
    // lettura, anche quando è attivo (vedi experiments/sigflags). Conta il comportamento.
    assert.notEqual(r.diag, NO_DIAG, 'il figlio non ha stampato la diagnostica dei segnali')
    assert.equal(JSON.parse(r.diag).SIGTERM.repairedByNapiGo, true)
  })
})
