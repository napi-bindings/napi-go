// Loads the binary built for this platform. Targets are named like the platform packages of
// napi-rs, <platform>-<arch>[-<abi>], from process.platform and process.arch plus the C library
// on Linux (gnu or musl) and on Windows (gnu: Go links with MinGW). Candidates, in order:
//   - NAPI_GO_NATIVE_LIBRARY_PATH, an explicit path (its errors propagate);
//   - <binaryName>.<target>.node and <binaryName>.node next to this file (local builds);
//   - the npm package <packageName>-<target> (published builds: an optional dependency).
// If none loads, the error lists every candidate with the reason.

const { existsSync, readFileSync } = require('node:fs')
const { join } = require('node:path')

// isMusl reports whether this Linux system uses musl libc. /usr/bin/ldd names its C library (a
// quick check); otherwise the process report has a glibc version only with glibc.
function isMusl() {
  try {
    const ldd = readFileSync('/usr/bin/ldd', 'latin1')
    if (ldd.includes('musl')) return true
    if (ldd.includes('GNU C Library')) return false
  } catch {
    // No readable ldd: ask the process report.
  }
  const report = process.report?.getReport?.()
  return report != null && typeof report === 'object' && !report.header?.glibcVersionRuntime
}

function currentTarget() {
  const { platform, arch } = process
  switch (platform) {
    case 'linux':
      return `linux-${arch}-${isMusl() ? 'musl' : 'gnu'}`
    case 'win32':
      return `win32-${arch}-gnu`
    default:
      return `${platform}-${arch}`
  }
}

function loadBinding() {
  const explicit = process.env.NAPI_GO_NATIVE_LIBRARY_PATH
  if (explicit) return require(explicit)
  const target = currentTarget()
  const failures = []
  for (const file of [`${binaryName}.${target}.node`, `${binaryName}.node`]) {
    const path = join(__dirname, file)
    if (!existsSync(path)) {
      failures.push({ candidate: path, reason: 'not found' })
      continue
    }
    try {
      return require(path)
    } catch (err) {
      failures.push({ candidate: path, reason: err.message, err })
    }
  }
  if (packageName !== null) {
    const pkg = `${packageName}-${target}`
    try {
      return require(pkg)
    } catch (err) {
      const missing = err.code === 'MODULE_NOT_FOUND' && err.message.includes(`'${pkg}'`)
      failures.push({ candidate: pkg, reason: missing ? 'package not installed' : err.message, err })
    }
  }
  let message = `cannot load the native addon ${binaryName} for ${target}`
  if (target.endsWith('-musl')) {
    message += ' (musl libc, as on Alpine Linux, is not supported: Go cannot build shared libraries that load with musl)'
  }
  message += ':' + failures.map((f) => `\n  ${f.candidate}: ${f.reason.split('\n')[0]}`).join('')
  const errors = failures.filter((f) => f.err).map((f) => f.err)
  if (errors.length === 0) throw new Error(message)
  throw new Error(message, { cause: errors.length === 1 ? errors[0] : new AggregateError(errors) })
}

const binding = loadBinding()
module.exports = binding
