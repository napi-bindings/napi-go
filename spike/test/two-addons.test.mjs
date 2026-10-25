// Q5 (rischio n.1): due addon napi-go compilati separatamente = due runtime Go nello
// stesso processo Node.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Worker } from 'node:worker_threads'
import { load, addonPath } from './helpers.mjs'

test('due runtime Go: caricamento e chiamate sincrone', () => {
  const b = load('basic')
  const s = load('second')
  assert.equal(s.name(), 'second')
  assert.equal(s.mul(6, 7), 42)
  assert.equal(b.add(6, 7), 13)
  // indirizzi diversi = immagini (e runtime) Go distinte
  assert.notEqual(s.goAddr().split(' ')[0], b.info().goAddr)
})

test('due runtime Go: input limite, null ed errori isolati per runtime', async () => {
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

test('due runtime Go: async, GC e CPU concorrenti in entrambi', async () => {
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

test('due runtime Go: entrambi caricati in worker concorrenti', async () => {
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
