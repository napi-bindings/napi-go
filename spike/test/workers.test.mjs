// Q4: worker_threads — l'init del modulo gira per ogni env, stato per env, cleanup alla
// terminazione del worker, nessuna interferenza col main thread.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Worker } from 'node:worker_threads'
import { load, addonPath } from './helpers.mjs'

const b = load('basic')

const workerSrc = (path, extra = '') => `
const { parentPort } = require('node:worker_threads')
const b = require(${JSON.stringify(path)})
parentPort.on('message', () => {}) // tiene vivo il worker finché il main non lo termina
;(async () => {
  const sync = b.add(20, 22)
  const t = b.threadInfo()
  const r = await b.sleep(5)
  const many = await Promise.all(Array.from({ length: 500 }, () => b.sleep(1)))
  let panicked = false
  try { b.panicNow('w') } catch (e) { panicked = e.code === 'GO_PANIC' }
  ${extra}
  parentPort.postMessage({ sync, t, r, n: many.length, panicked, info: b.info() })
})()
`

function runWorker(src) {
  return new Promise((resolve, reject) => {
    const w = new Worker(src, { eval: true })
    w.once('message', (m) => { resolve({ w, m }) })
    w.once('error', reject)
  })
}

test('worker: env separato con proprio thread JS e dispatcher', async () => {
  const mainThread = b.threadInfo().jsThread
  const { w, m } = await runWorker(workerSrc(addonPath('basic')))
  assert.equal(m.sync, 42)
  assert.equal(m.n, 500)
  assert.equal(m.panicked, true)
  assert.notEqual(m.t.jsThread, mainThread)       // un altro thread JS
  assert.equal(m.r.resolveThread, m.t.jsThread)   // risolta sul thread del worker
  assert.equal(m.info.goAddr, b.info().goAddr)    // stesso runtime Go del main
  await w.terminate()
})

test('worker: 8 worker concorrenti, terminazione e cleanup degli env', async () => {
  const before = b.info()
  const ws = await Promise.all(Array.from({ length: 8 }, () => runWorker(workerSrc(addonPath('basic')))))
  assert.equal(b.info().liveEnvs, before.liveEnvs + 8)
  await Promise.all(ws.map(({ w }) => w.terminate()))
  // il cleanup hook gira alla distruzione dell'env del worker
  await new Promise((r) => setTimeout(r, 50))
  const after = b.info()
  assert.equal(after.liveEnvs, before.liveEnvs)
  assert.equal(after.totalEnvs, before.totalEnvs + 8)
  assert.equal(b.add(1, 1), 2) // il main continua a funzionare
})

test('worker: terminazione con Promise ancora pendenti (ctx cancellato, nessun crash)', async () => {
  const src = `
    const { parentPort } = require('node:worker_threads')
    const b = require(${JSON.stringify(addonPath('basic'))})
    for (let i = 0; i < 200; i++) b.sleep(10_000)
    b.burnCPU(2_000)
    parentPort.postMessage('started')`
  for (let round = 0; round < 5; round++) {
    const w = new Worker(src, { eval: true })
    await new Promise((r) => w.once('message', r))
    await w.terminate()
  }
  await new Promise((r) => setTimeout(r, 50))
  assert.equal(b.info().liveEnvs, 1)
  assert.equal((await b.sleep(1)).ms, 1)
})
