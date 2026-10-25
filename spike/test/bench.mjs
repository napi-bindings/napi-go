// Q8: costo per chiamata JS → nativo. Confronta funzione JS, addon C puro (cref) e addon Go.
//   node test/bench.mjs [--json]
import { existsSync } from 'node:fs'
import { load, addonPath } from './helpers.mjs'

const go = load('basic')
const c = existsSync(addonPath('cref')) ? load('cref') : null
const js = { noop() {}, add: (a, b) => a + b, echo: (s) => s }

const short = 'hello napi-go!!!' // 16 byte
const kb = 'x'.repeat(1024)
const cases = [
  ['noop()', (m) => () => m.noop()],
  ['add(1.5, 2.5)', (m) => () => m.add(1.5, 2.5)],
  ['echo(16 B)', (m) => () => m.echo(short)],
  ['echo(1 KiB)', (m) => () => m.echo(kb)],
  ['addTyped(1.5, 2.5)', (m) => () => (m.addTyped ?? m.add)(1.5, 2.5)],
  ['echoTyped(16 B)', (m) => () => (m.echoTyped ?? m.echo)(short)],
]

function bench(fn, iters) {
  for (let i = 0; i < 20_000; i++) fn() // warmup
  let best = Infinity
  for (let round = 0; round < 5; round++) {
    const t0 = process.hrtime.bigint()
    for (let i = 0; i < iters; i++) fn()
    const ns = Number(process.hrtime.bigint() - t0) / iters
    best = Math.min(best, ns)
  }
  return best
}

const ITERS = 1_000_000
const rows = []
for (const [name, mk] of cases) {
  const row = { case: name, js: bench(mk(js), ITERS) }
  if (c) row.c = bench(mk(c), ITERS)
  const m0 = go.goMallocs()
  row.go = bench(mk(go), ITERS)
  row.goAllocsPerCall = (go.goMallocs() - m0 - 1) / (ITERS * 5 + 20_000)
  if (c) row.goOverC = row.go - row.c
  rows.push(row)
}

// Throughput Promise: N sleep(0) concorrenti.
const N = 100_000
const t0 = performance.now()
await Promise.all(Array.from({ length: N }, () => go.sleep(0)))
const promiseUs = ((performance.now() - t0) * 1000) / N

if (process.argv.includes('--json')) {
  console.log(JSON.stringify({ node: process.version, platform: `${process.platform}/${process.arch}`, rows, promiseUs }))
} else {
  console.log(`Node ${process.version} ${process.platform}/${process.arch} — ns per chiamata (migliore di 5 round da ${ITERS})`)
  console.table(rows.map((r) => Object.fromEntries(Object.entries(r).map(([k, v]) => [k, typeof v === 'number' ? +v.toFixed(k === 'goAllocsPerCall' ? 3 : 1) : v]))))
  console.log(`Promise Go (sleep(0)) x${N}: ${promiseUs.toFixed(2)} µs/promise`)
}
