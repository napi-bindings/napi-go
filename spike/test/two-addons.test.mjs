// Q5 (rischio n.1): due addon napi-go compilati separatamente = due runtime Go nello
// stesso processo Node.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import { Worker } from 'node:worker_threads'
import { load, addonPath } from './helpers.mjs'

// Su darwin/amd64 tutti i runtime Go tengono g nello stesso slot TLS fisso 0x30(GS): il secondo
// runtime trova il g del primo sul main thread e lo usa come proprio (heap e M mescolati,
// "fatal error: bad sweepgen in refill"). Limite di Go, vedi README, scoperta 8.
const DARWIN_X64 = process.platform === 'darwin' && process.arch === 'x64'
const SKIP_IF_SHARED_G_SLOT = DARWIN_X64 && 'darwin/amd64: due runtime Go condividono lo slot TLS di g (limite di Go)'
const CHILD_TIMEOUT_MS = 30_000
const SYSMON_RETAKE_WAIT_MS = 30 // > ForcePreemptNS (10 ms) di Go: sysmon riprende la P da _Psyscall
const NULL_POINTER_TEXT = '0x0'

test('darwin/amd64: il secondo runtime Go riusa il g del primo (slot TLS condiviso)', {
  skip: !DARWIN_X64 && 'solo darwin/amd64: altrove ogni runtime ha il proprio slot',
}, (t) => {
  // In un processo separato: lo stato dei runtime dopo il test non è affidabile.
  // Scriviamo beforeSecond su fd 1 prima di require('second'): l'ingresso nel secondo runtime
  // con il g del primo può andare in crash ("fatal error: bad sweepgen in refill") già dentro
  // goNapiRegisterModule di second se la P di basic è ancora in _Psyscall, non solo all'uscita.
  const src = `
    const fs = require('node:fs')
    const b = require(${JSON.stringify(addonPath('basic'))})
    const beforeSecond = b.__goTlsSlot()  // init di basic: il suo g resta legato al main thread
    fs.writeSync(1, beforeSecond + '\\n')
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ${SYSMON_RETAKE_WAIT_MS})
    require(${JSON.stringify(addonPath('second'))})  // anche l'init di second entra in Go
    const afterSecond = b.__goTlsSlot()
    fs.writeSync(1, afterSecond + '\\n')`
  const r = spawnSync(process.execPath, ['-e', src], { encoding: 'utf8', timeout: CHILD_TIMEOUT_MS })
  assert.equal(r.error, undefined)
  const lines = (r.stdout || '').trim().split('\n').filter(Boolean)
  const [beforeSecond, afterSecond] = lines
  const firstErrLine = (r.stderr || '').split('\n')[0] || '-'
  const outcome = `code=${r.status} sig=${r.signal} stdout=${JSON.stringify(lines)} stderr: ${firstErrLine}`
  t.diagnostic(outcome)
  assert.ok(beforeSecond, `il processo figlio non ha scritto beforeSecond: ${outcome}`)
  assert.notEqual(beforeSecond, NULL_POINTER_TEXT, 'il runtime di basic deve aver legato un g al main thread')
  if (afterSecond !== undefined) {
    // Se second avesse un proprio slot avrebbe legato il suo g e lo slot sarebbe cambiato.
    assert.equal(afterSecond, beforeSecond)
  } else {
    // Crash già durante l'init di second per aver riusato g/mcache di basic (es. bad sweepgen in refill).
    assert.match(r.stderr || '', /^fatal error:/m, `atteso fatal error del runtime Go: ${outcome}`)
  }
})

test('due runtime Go: caricamento e chiamate sincrone', { skip: SKIP_IF_SHARED_G_SLOT }, () => {
  const b = load('basic')
  const s = load('second')
  assert.equal(s.name(), 'second')
  assert.equal(s.mul(6, 7), 42)
  assert.equal(b.add(6, 7), 13)
  // indirizzi diversi = immagini (e runtime) Go distinte
  assert.notEqual(s.goAddr().split(' ')[0], b.info().goAddr)
})

test('due runtime Go: input limite, null ed errori isolati per runtime', { skip: SKIP_IF_SHARED_G_SLOT }, async () => {
  const b = load('basic')
  const s = load('second')
  assert.equal(s.mul(Number.MAX_VALUE, 2), Infinity)
  assert.ok(Object.is(s.mul(-0, 1), -0))
  assert.ok(Number.isNaN(s.mul(NaN, 1)))
  assert.throws(() => s.mul(null, 1), /A number was expected/)
  assert.throws(() => s.mul(), /A number was expected/)
  assert.equal(await s.sleep(0), 0)
  // un panic nel runtime A non tocca il runtime B
  assert.throws(() => b.panicNow(''), { code: 'GO_PANIC', message: 'boom: ' })
  await assert.rejects(b.panicAsync(), { code: 'GO_PANIC' })
  assert.equal(s.mul(3, 3), 9)
  assert.equal(b.echo(''), '')
})

test('due runtime Go: async, GC e CPU concorrenti in entrambi', { skip: SKIP_IF_SHARED_G_SLOT }, async () => {
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

test('due runtime Go: entrambi caricati in worker concorrenti', { skip: SKIP_IF_SHARED_G_SLOT }, async () => {
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
