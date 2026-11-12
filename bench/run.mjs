// Runs the benchmarks on the implementations built by build.mjs and prints a Markdown table of
// the median time per operation.
//
//   node bench/run.mjs [--quick] [--json <file>] [--filter <regexp>]
//
// Every implementation is first checked to return the same results (and, for the native ones, to
// throw for an argument of the wrong type: napi-go and node-addon-api throw a TypeError, napi-rs
// an Error). The samples of the implementations are interleaved (one round runs every
// implementation once), so that a change of machine load affects all of them alike.
import { createRequire } from 'node:module'
import { existsSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { cpus, platform, arch } from 'node:os'
import assert from 'node:assert/strict'

const require = createRequire(import.meta.url)
const buildDir = join(dirname(fileURLToPath(import.meta.url)), 'build')

const KiB = 1024
const MiB = 1024 * KiB
const NS_PER_MS = 1e6
// A Promise.all batch of the async throughput case.
const BATCH = 1000

const args = process.argv.slice(2)
const flag = (name) => args.includes(name)
const option = (name) => {
  const i = args.indexOf(name)
  return i >= 0 ? args[i + 1] : undefined
}
const quick = flag('--quick')
const filter = option('--filter') ? new RegExp(option('--filter')) : null
const jsonFile = option('--json')

// Each sample lasts about sampleMs; the first warmupRounds rounds are discarded.
const sampleMs = quick ? 20 : 100
const rounds = quick ? 5 : 15
const warmupRounds = quick ? 1 : 3

// The implementations, in the columns of the table. Plain JS is a reference, not a competitor:
// it shows the cost of the JS side of each case.
const implementations = [
  { name: 'napi-go', file: 'napigo.node' },
  { name: 'napi-rs', file: 'napi-rs.node' },
  { name: 'node-addon-api', file: 'node-addon-api.node' }
].filter(({ name, file }) => {
  if (existsSync(join(buildDir, file))) return true
  process.stderr.write(`${name}: ${file} not built, skipped\n`)
  return false
}).map(({ name, file }) => ({ name, addon: require(join(buildDir, file)) }))

implementations.push({
  name: 'plain JS',
  reference: true,
  addon: {
    noop () {},
    add: (a, b) => a + b,
    echo: (s) => s,
    bufferLength: (b) => b.length,
    makeBuffer: (n) => Buffer.alloc(n),
    resolveAsync: async (n) => n
  }
})

const strings = { '16 B': 'x'.repeat(16), '1 KiB': 'x'.repeat(KiB), '64 KiB': 'x'.repeat(64 * KiB) }
const megabyte = Buffer.alloc(MiB, 1)

// A case returns, for an addon, a function running one operation (sync) or a function running n
// operations (async: it returns a Promise).
const cases = [
  { name: 'noop()', sync: (a) => () => a.noop() },
  { name: 'add(a, b)', sync: (a) => { let i = 0; return () => a.add(i++, 1) } },
  ...Object.entries(strings).map(([size, s]) => ({ name: `echo(string of ${size})`, sync: (a) => () => a.echo(s) })),
  { name: 'bufferLength(Buffer of 1 MiB)', sync: (a) => () => a.bufferLength(megabyte) },
  { name: 'makeBuffer(1 MiB)', sync: (a) => () => a.makeBuffer(MiB) },
  {
    name: 'await resolveAsync(n), one at a time',
    async: (a) => async (n) => { for (let i = 0; i < n; i++) await a.resolveAsync(i) }
  },
  {
    name: `resolveAsync(n) × ${BATCH} with Promise.all`,
    async: (a) => async (n) => {
      for (let i = 0; i < n; i++) await Promise.all(Array.from({ length: BATCH }, (_, j) => a.resolveAsync(j)))
    },
    perCall: BATCH
  }
].filter((c) => !filter || filter.test(c.name))

// check verifies that addon behaves like the reference implementation.
async function check ({ name, addon, reference }) {
  assert.equal(addon.noop(), undefined, `${name}: noop`)
  assert.equal(addon.add(2, 3), 5, `${name}: add`)
  for (const s of Object.values(strings)) assert.equal(addon.echo(s), s, `${name}: echo`)
  if (!reference) {
    assert.throws(() => addon.add('2', 3), Error, `${name}: add with a string`)
    assert.throws(() => addon.echo(1), Error, `${name}: echo with a number`)
  }
  assert.equal(addon.bufferLength(megabyte), MiB, `${name}: bufferLength`)
  const made = addon.makeBuffer(MiB)
  assert.ok(made instanceof Uint8Array && made.length === MiB && made.every((b) => b === 0), `${name}: makeBuffer`)
  assert.equal(await addon.resolveAsync(7), 7, `${name}: resolveAsync`)
}

const now = () => process.hrtime.bigint()

// time returns the nanoseconds taken by n operations.
async function time (c, op, n) {
  const start = now()
  if (c.sync) {
    for (let i = 0; i < n; i++) op()
  } else {
    await op(n)
  }
  return Number(now() - start)
}

// calibrate returns the number of operations of a sample of about sampleMs.
async function calibrate (c, op) {
  let n = 1
  for (;;) {
    const ns = await time(c, op, n)
    if (ns >= sampleMs * NS_PER_MS / 10) return Math.max(1, Math.round(n * sampleMs * NS_PER_MS / ns))
    n *= 10
  }
}

const median = (xs) => {
  const s = [...xs].sort((a, b) => a - b)
  const m = s.length >> 1
  return s.length % 2 ? s[m] : (s[m - 1] + s[m]) / 2
}

function format (ns) {
  if (ns < 1e3) return `${ns.toFixed(ns < 10 ? 1 : 0)} ns`
  if (ns < 1e6) return `${(ns / 1e3).toFixed(ns < 1e4 ? 2 : 1)} µs`
  return `${(ns / 1e6).toFixed(2)} ms`
}

async function measure (c) {
  const runs = implementations.map((impl) => ({ impl, op: (c.sync || c.async)(impl.addon), samples: [] }))
  for (const r of runs) r.n = await calibrate(c, r.op)
  for (let round = 0; round < warmupRounds + rounds; round++) {
    for (const r of runs) {
      const ns = await time(c, r.op, r.n)
      if (round >= warmupRounds) r.samples.push(ns / r.n / (c.perCall ?? 1))
    }
  }
  return Object.fromEntries(runs.map((r) => [r.impl.name, median(r.samples)]))
}

for (const impl of implementations) await check(impl)

const results = []
for (const c of cases) {
  results.push({ case: c.name, nsPerOp: await measure(c) })
  process.stderr.write('.')
}
process.stderr.write('\n')

const names = implementations.map((i) => i.name)
const env = `Node.js ${process.version}, ${platform()}/${arch()}, ${cpus()[0]?.model.trim() ?? 'unknown CPU'}`
const lines = [
  `Median time per operation (${rounds} samples of about ${sampleMs} ms). ${env}.`,
  '',
  `| Case | ${names.join(' | ')} |`,
  `|---|${names.map(() => '---:').join('|')}|`,
  ...results.map((r) => `| ${r.case} | ${names.map((n) => format(r.nsPerOp[n])).join(' | ')} |`)
]
process.stdout.write(lines.join('\n') + '\n')
if (jsonFile) writeFileSync(jsonFile, JSON.stringify({ env, rounds, sampleMs, results }, null, 2) + '\n')
