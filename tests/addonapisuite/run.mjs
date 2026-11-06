// Builds the Go port of node-addon-api's test binding and runs the copied node-addon-api scripts.
//
//   node tests/addonapisuite/run.mjs [options] [filter...]
//
// Options:
//   --no-build   run without rebuilding the binding
//   --build-only build without running
//
// A filter selects tests whose id (the script path under test/, without extension, e.g.
// "basic_types/number") contains the given substring.
//
// Each script runs in its own process with the flags node-addon-api's test/index.js uses
// (--expose-gc --no-concurrent-array-buffer-sweeping), from this directory, so that
// test/common/index.js finds test/build/Release/binding.node. A test passes if the process exits
// with code 0. status.json lists the scripts that are skipped, with the reason.
// Honored environment variables: CC (e.g. "zig cc" on Windows), GOEXPERIMENT (e.g. cgocheck2),
// NAPI_VERSION (declared Node-API version, see versionTag).
import { spawn, spawnSync } from 'node:child_process'
import { existsSync, readdirSync, readFileSync, rmSync, mkdirSync, statSync } from 'node:fs'
import { dirname, join, relative, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, '..', '..')
const testDir = join(here, 'test')
const binding = join(testDir, 'build', 'Release', 'binding.node')
const NODE_FLAGS = ['--expose-gc', '--no-concurrent-array-buffer-sweeping']
const TEST_TIMEOUT_MS = 120_000
// Maximum wait for 'close' after killing a timed-out test.
const KILL_GRACE_MS = 5_000
const EXIT_FAILURE = 1
const isWindows = process.platform === 'win32'
// Files and directories under test/ that are helpers rather than tests.
const HELPERS = new Set(['build', 'common', 'child_processes', 'testUtil.js', 'napi_child.js'])
// sys build tags for the Node-API versions above sys's default (8); newer Node.js versions get the
// highest one.
const VERSION_TAGS = new Map([[9, 'napi_go_v9'], [10, 'napi_go_v10']])
const MAX_VERSION_TAG = 10
// Environment variables stripped from test processes: they alter Node.js behavior.
const STRIPPED_ENV = ['NODE_OPTIONS']

class UsageError extends Error {}

function parseArgs (argv) {
  const opts = { build: true, run: true, filters: [] }
  for (const a of argv) {
    if (a === '--no-build') opts.build = false
    else if (a === '--build-only') opts.run = false
    else if (a.startsWith('--')) throw new UsageError(`unknown option: ${a}`)
    else opts.filters.push(a)
  }
  return opts
}

function readStatus () {
  const file = join(here, 'status.json')
  try {
    return JSON.parse(readFileSync(file, 'utf8'))
  } catch (err) {
    throw new Error(`cannot read ${relative(root, file)}: ${err.message}`)
  }
}

// discover returns the ids of the test scripts under test/, sorted.
function discover (dir = testDir, prefix = '') {
  const ids = []
  for (const name of readdirSync(dir).sort()) {
    if (prefix === '' && HELPERS.has(name)) continue
    const path = join(dir, name)
    if (statSync(path).isDirectory()) ids.push(...discover(path, `${prefix}${name}/`))
    else if (name.endsWith('.js')) ids.push(prefix + name.slice(0, -'.js'.length))
  }
  return ids
}

// run executes a command and collects its result without throwing. On timeout the whole process
// tree is killed: scripts spawn child processes that inherit the stdio pipes.
function run (cmd, args, options) {
  return new Promise((resolve) => {
    const child = spawn(cmd, args, { ...options, stdio: ['ignore', 'pipe', 'pipe'], detached: !isWindows })
    let stdout = ''
    let stderr = ''
    let timedOut = false
    let exit = { code: null, signal: null }
    let graceTimer = null
    const finish = (r) => { clearTimeout(timer); clearTimeout(graceTimer); resolve(r) }
    const timer = options.timeout
      ? setTimeout(() => {
        timedOut = true
        killTree(child)
        graceTimer = setTimeout(() => finish({ ...exit, stdout, stderr, timedOut }), KILL_GRACE_MS)
      }, options.timeout)
      : null
    child.stdout.on('data', (d) => { stdout += d })
    child.stderr.on('data', (d) => { stderr += d })
    child.on('exit', (code, signal) => { exit = { code, signal } })
    child.on('error', (err) => finish({ code: null, signal: null, stdout, stderr: stderr + err.message, timedOut }))
    child.on('close', (code, signal) => finish({ code, signal, stdout, stderr, timedOut }))
  })
}

function killTree (child) {
  if (isWindows) {
    const r = spawnSync('taskkill', ['/pid', String(child.pid), '/T', '/F'], { encoding: 'utf8' })
    if (r.error || r.status !== 0) child.kill('SIGKILL')
    return
  }
  try {
    process.kill(-child.pid, 'SIGKILL')
  } catch (err) {
    if (err.code !== 'ESRCH') process.stderr.write(`cannot kill process group ${child.pid}: ${err.message}\n`)
    child.kill('SIGKILL')
  }
}

// versionTag returns the sys build tag for the Node-API version the binding declares. Like
// node-addon-api's common.gypi, it is NAPI_VERSION if set, otherwise the version of the running
// Node.js (the scripts read the same variable to skip what the version lacks).
function versionTag () {
  const napi = Number(process.env.NAPI_VERSION || process.versions.napi)
  if (napi >= MAX_VERSION_TAG) return `napi_go_v${MAX_VERSION_TAG}`
  return VERSION_TAGS.get(napi) ?? null
}

async function build () {
  mkdirSync(dirname(binding), { recursive: true })
  const pkg = './' + relative(root, join(here, 'binding')).split(sep).join('/')
  const args = ['build', '-buildmode=c-shared', '-buildvcs=false', '-trimpath', '-ldflags=-s -w']
  const tag = versionTag()
  if (tag) args.push(`-tags=${tag}`)
  args.push('-o', binding, pkg)
  const t0 = Date.now()
  const r = await run('go', args, { cwd: root, env: { ...process.env, CGO_ENABLED: '1' } })
  // c-shared also writes a C header for exports, which is not needed.
  const header = binding.replace(/\.node$/, '.h')
  if (existsSync(header)) rmSync(header)
  if (r.code !== 0) {
    report('BUILD FAIL', 'binding', r)
    process.exit(EXIT_FAILURE)
  }
  process.stdout.write(`built binding.node in ${Date.now() - t0} ms\n`)
}

function report (label, id, r) {
  process.stdout.write(`${label} ${id}\n`)
  const reason = r.timedOut ? `timeout after ${TEST_TIMEOUT_MS} ms` : `exit code=${r.code} signal=${r.signal}`
  process.stdout.write(`  ${reason}\n`)
  if (r.stdout.trim()) process.stdout.write(`  --- stdout\n${r.stdout.trimEnd()}\n`)
  if (r.stderr.trim()) process.stdout.write(`  --- stderr\n${r.stderr.trimEnd()}\n`)
}

function testEnv () {
  const env = { ...process.env }
  for (const name of STRIPPED_ENV) delete env[name]
  return env
}

async function main () {
  const opts = parseArgs(process.argv.slice(2))
  const status = readStatus()
  const ids = discover().filter((id) => opts.filters.length === 0 || opts.filters.some((f) => id.includes(f)))
  if (ids.length === 0) throw new UsageError('no test matches the given filters')
  if (opts.build) await build()
  if (!opts.run) return

  const counts = { pass: 0, skip: 0, fail: 0 }
  for (const id of ids) {
    const reason = status.skip?.[id]
    if (reason) {
      counts.skip++
      process.stdout.write(`skip ${id} (${reason})\n`)
      continue
    }
    const file = join(testDir, ...id.split('/')) + '.js'
    const r = await run(process.execPath, [...NODE_FLAGS, file], { cwd: here, env: testEnv(), timeout: TEST_TIMEOUT_MS })
    if (r.code === 0 && !r.timedOut) {
      counts.pass++
      process.stdout.write(`ok   ${id}\n`)
    } else {
      counts.fail++
      report('FAIL', id, r)
    }
  }
  process.stdout.write(`\n${counts.pass} passed, ${counts.skip} skipped, ${counts.fail} failed\n`)
  if (counts.fail) process.exit(EXIT_FAILURE)
}

main().catch((err) => {
  process.stderr.write(`${err instanceof UsageError ? 'usage error' : 'error'}: ${err.message}\n`)
  process.exit(EXIT_FAILURE)
})
