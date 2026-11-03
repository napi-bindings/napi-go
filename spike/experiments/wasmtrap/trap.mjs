// Cost of a WebAssembly trap (out-of-bounds access → RuntimeError) with and without
// the Go addon loaded and with the Go runtime under load. Diagnostics for Q6 on Windows.
//   node experiments/wasmtrap/trap.mjs
import { createRequire } from 'node:module'
import { availableParallelism } from 'node:os'

const require = createRequire(import.meta.url)
// (module (memory 1) (func (export "load") (param i32) (result i32) local.get 0 i32.load))
const wasm = new Uint8Array([0, 97, 115, 109, 1, 0, 0, 0, 1, 6, 1, 96, 1, 127, 1, 127, 3, 2, 1, 0, 5, 3, 1, 0, 1,
  7, 8, 1, 4, 108, 111, 97, 100, 0, 0, 10, 9, 1, 7, 0, 32, 0, 40, 2, 0, 11])
const { instance } = await WebAssembly.instantiate(wasm)
const load = instance.exports.load

function run(label, n = 2000) {
  const t0 = process.hrtime.bigint()
  let caught = 0
  let worst = 0n
  for (let i = 0; i < n; i++) {
    const s = process.hrtime.bigint()
    try { load(1 << 20) } catch (e) { if (e instanceof WebAssembly.RuntimeError) caught++ }
    const d = process.hrtime.bigint() - s
    if (d > worst) worst = d
  }
  const avg = Number(process.hrtime.bigint() - t0) / 1000 / n
  console.log(`${label.padEnd(36)} ${caught}/${n} traps  avg ${avg.toFixed(1)} µs  worst ${(Number(worst) / 1000).toFixed(0)} µs`)
}

console.log(`${process.platform}/${process.arch} node ${process.version} cpu ${availableParallelism()}`)
run('without addon')
const b = require('../../build/basic.node')
run('addon loaded')
const burning = b.burnCPU(4000)
run('addon + burnCPU (Go saturates Ps)')
await burning
run('after burnCPU')
