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

test('Q6: trap WebAssembly (SIGSEGV di V8) mentre il runtime Go fa preemption', async () => {
  const b = load('basic')
  const { instance } = await WebAssembly.instantiate(wasmBytes)
  const burning = b.burnCPU(500) // goroutine su tutte le P: segnali SIGURG + GC Go
  let traps = 0
  const t0 = Date.now()
  while (Date.now() - t0 < 400) {
    try { instance.exports.load(1 << 20) } catch (e) { if (e instanceof WebAssembly.RuntimeError) traps++ }
    assert.equal(instance.exports.load(0), 0)
  }
  await burning
  assert.ok(traps > 1000, `trap gestite: ${traps}`)
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

test('Q6: process.exit con goroutine in volo non crasha', () => {
  const r = nodeEval(`const b = require(${JSON.stringify(addonPath('basic'))});
    for (let i = 0; i < 1000; i++) b.sleep(5);
    b.burnCPU(1000);
    setTimeout(() => process.exit(7), 20)`)
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

test('Q6: SIGINT gestito da process.on e SIGTERM di default', { skip: isWin && 'segnali POSIX' }, async () => {
  const script = `const b = require(${JSON.stringify(addonPath('basic'))});
    process.on('SIGINT', () => { console.log('got SIGINT'); process.exit(42) });
    b.burnCPU(60000); setInterval(() => b.add(1,1), 5); console.log('ready')`
  const runChild = (signal) => new Promise((resolve, reject) => {
    const c = spawn(process.execPath, ['-e', script], { stdio: ['ignore', 'pipe', 'pipe'] })
    c.once('error', reject)
    let out = ''
    c.stdout.on('data', (d) => {
      out += d
      if (out.includes('ready')) c.kill(signal)
    })
    c.on('exit', (code, sig) => resolve({ code, sig, out }))
  })
  const a = await runChild('SIGINT')
  assert.equal(a.code, 42)
  assert.match(a.out, /got SIGINT/)
  const t = await runChild('SIGTERM')
  assert.equal(t.sig, 'SIGTERM')
})
