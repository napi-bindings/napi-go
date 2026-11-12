// Builds the three implementations of the benchmark addon into bench/build/:
//   napigo.node          go build -buildmode=c-shared (release flags of napi-go build)
//   napi-rs.node         cargo build --release (skipped without cargo)
//   node-addon-api.node  the C++ compiler with the Node.js headers (skipped on Windows, which
//                        needs node.lib and node-gyp)
// With BENCH_REQUIRE_ALL=1 a skipped implementation is an error (CI).
import { execFileSync } from 'node:child_process'
import { copyFileSync, existsSync, mkdirSync, rmSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const benchDir = dirname(fileURLToPath(import.meta.url))
const repoRoot = join(benchDir, '..')
const buildDir = join(benchDir, 'build')
const requireAll = process.env.BENCH_REQUIRE_ALL === '1'

// Shared library names of cargo, per platform.
const cdylib = { linux: 'libbench_napi_rs.so', darwin: 'libbench_napi_rs.dylib', win32: 'bench_napi_rs.dll' }

function run (cmd, args, cwd) {
  execFileSync(cmd, args, { cwd, stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '1' } })
}

function hasCommand (cmd) {
  try {
    execFileSync(cmd, ['--version'], { stdio: 'ignore' })
    return true
  } catch {
    return false
  }
}

function skip (name, reason) {
  if (requireAll) throw new Error(`${name}: ${reason} (BENCH_REQUIRE_ALL=1)`)
  process.stdout.write(`skipped ${name}: ${reason}\n`)
}

function buildNapiGo () {
  const out = join(buildDir, 'napigo.node')
  run('go', ['build', '-buildmode=c-shared', '-trimpath', '-ldflags=-s -w', '-o', out, './bench/napigo'], repoRoot)
  rmSync(join(buildDir, 'napigo.h'), { force: true })
  process.stdout.write('built napigo.node\n')
}

function buildNapiRs () {
  if (!hasCommand('cargo')) return skip('napi-rs.node', 'cargo not found')
  const lib = cdylib[process.platform]
  if (!lib) return skip('napi-rs.node', `no cdylib name for ${process.platform}`)
  const targetDir = join(buildDir, 'cargo')
  run('cargo', ['build', '--release', '--locked', '--manifest-path', join(benchDir, 'napi-rs', 'Cargo.toml'), '--target-dir', targetDir], benchDir)
  copyFileSync(join(targetDir, 'release', lib), join(buildDir, 'napi-rs.node'))
  process.stdout.write('built napi-rs.node\n')
}

function buildNodeAddonApi () {
  if (process.platform === 'win32') return skip('node-addon-api.node', 'Windows needs node-gyp and node.lib')
  const headers = join(dirname(process.execPath), '..', 'include', 'node')
  if (!existsSync(join(headers, 'node_api.h'))) return skip('node-addon-api.node', `no Node.js headers in ${headers}`)
  const addonApi = join(benchDir, 'node_modules', 'node-addon-api')
  if (!existsSync(join(addonApi, 'napi.h'))) return skip('node-addon-api.node', 'node-addon-api not installed: run npm ci in bench/')
  const cxx = process.env.CXX || 'c++'
  const args = ['-std=c++17', '-O3', '-shared', '-fPIC', '-fvisibility=hidden',
    '-DNAPI_VERSION=8', '-DNODE_ADDON_API_DISABLE_CPP_EXCEPTIONS',
    '-I', headers, '-I', addonApi,
    '-o', join(buildDir, 'node-addon-api.node'), join(benchDir, 'node-addon-api', 'addon.cc')]
  // Node-API symbols are resolved when Node.js loads the addon.
  if (process.platform === 'darwin') args.push('-undefined', 'dynamic_lookup')
  run(cxx, args, benchDir)
  process.stdout.write('built node-addon-api.node\n')
}

mkdirSync(buildDir, { recursive: true })
buildNapiGo()
buildNapiRs()
buildNodeAddonApi()
