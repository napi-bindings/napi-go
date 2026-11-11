// End-to-end tests of the platform loader of the generated index.js (internal/codegen/loader.js),
// with the codegen addon: which binary it picks, the platform packages, the errors, and the
// export names for ES modules. Each case runs in a child process, in a temporary directory: a
// loaded addon cannot be deleted on Windows.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

import { addonPath, nodeEval, lastJSONLine } from './helpers.mjs'

const entryPoint = join(dirname(fileURLToPath(import.meta.url)), 'addons', 'codegen', 'index.js')

// withDir runs fn in a new temporary directory holding the entry point, and removes it.
function withDir (fn, entrySource = readFileSync(entryPoint, 'utf8')) {
  const dir = mkdtempSync(join(tmpdir(), 'napi-go-loader-'))
  try {
    writeFileSync(join(dir, 'index.js'), entrySource)
    return fn(dir)
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
}

// requireIn loads the entry point of dir in a child and returns { ok, sum } or { ok: false, message, cause }.
function requireIn (dir, { env, nodeArgs } = {}) {
  const code = `
    let out
    try {
      out = { ok: true, sum: require(${JSON.stringify(join(dir, 'index.js'))}).add(2, 3) }
    } catch (err) {
      out = { ok: false, message: err.message, code: err.code ?? null, cause: err.cause ? err.cause.constructor.name : null }
    }
    process.stdout.write(JSON.stringify(out) + '\\n')`
  const r = nodeEval(code, { env, nodeArgs })
  assert.equal(r.status, 0, r.stderr)
  return lastJSONLine(r)
}

// The target of this machine, as the loader computes it: from the error of an empty directory.
const target = withDir((dir) => /for (\S+):/.exec(requireIn(dir).message)[1])

test('the loader names the target after process.platform and process.arch', () => {
  assert.ok(target.startsWith(`${process.platform}-${process.arch}`), target)
})

test('without a binary the error lists every candidate, without a cause', () => {
  withDir((dir) => {
    const r = requireIn(dir)
    assert.equal(r.ok, false)
    // The directory as the loader sees it may differ from dir (realpath of the temporary
    // directory on macOS and Windows): the candidates are compared by file name.
    const [head, ...lines] = r.message.split('\n')
    assert.equal(head, `cannot load the native addon codegen for ${target}:`)
    assert.deepEqual(lines.map((l) => l.replace(/^ {2}.*[\\/]/, '')), [`codegen.${target}.node: not found`, 'codegen.node: not found'])
    assert.equal(r.cause, null)
  })
})

test('the binary named after the target comes before the plain one', () => {
  withDir((dir) => {
    copyFileSync(addonPath('codegen'), join(dir, `codegen.${target}.node`))
    writeFileSync(join(dir, 'codegen.node'), 'not a binary')
    assert.deepEqual(requireIn(dir), { ok: true, sum: 5 })
  })
})

test('the plain binary of a local build is the fallback', () => {
  withDir((dir) => {
    copyFileSync(addonPath('codegen'), join(dir, 'codegen.node'))
    assert.deepEqual(requireIn(dir), { ok: true, sum: 5 })
  })
})

test('a binary that fails to load is reported with its error as cause', () => {
  withDir((dir) => {
    writeFileSync(join(dir, `codegen.${target}.node`), '')
    const r = requireIn(dir)
    assert.equal(r.ok, false)
    assert.match(r.message, new RegExp(`codegen\\.${target}\\.node: \\S`))
    assert.doesNotMatch(r.message, new RegExp(`codegen\\.${target}\\.node: not found`))
    assert.equal(r.cause, 'Error')
  })
})

test('NAPI_GO_NATIVE_LIBRARY_PATH overrides the candidates and its errors propagate', () => {
  withDir((dir) => {
    writeFileSync(join(dir, 'codegen.node'), 'not a binary')
    assert.deepEqual(requireIn(dir, { env: { NAPI_GO_NATIVE_LIBRARY_PATH: addonPath('codegen') } }), { ok: true, sum: 5 })
    const missing = requireIn(dir, { env: { NAPI_GO_NATIVE_LIBRARY_PATH: join(dir, 'missing.node') } })
    assert.equal(missing.code, 'MODULE_NOT_FOUND')
    assert.doesNotMatch(missing.message, /cannot load the native addon/)
  })
})

// The entry point of an addon published as codegen-pkg (the generator writes the name of
// package.json; the test addon has none).
const publishedEntry = () => readFileSync(entryPoint, 'utf8').replace('const packageName = null', "const packageName = 'codegen-pkg'")

test('a published addon loads its platform package, after the local binaries', () => {
  withDir((dir) => {
    const before = requireIn(dir)
    assert.match(before.message, new RegExp(`\\n  codegen-pkg-${target}: package not installed$`))
    const pkgDir = join(dir, 'node_modules', `codegen-pkg-${target}`)
    mkdirSync(pkgDir, { recursive: true })
    copyFileSync(addonPath('codegen'), join(pkgDir, `codegen.${target}.node`))
    writeFileSync(join(pkgDir, 'package.json'), JSON.stringify({ name: `codegen-pkg-${target}`, version: '1.0.0', main: `codegen.${target}.node` }))
    assert.deepEqual(requireIn(dir), { ok: true, sum: 5 })
  }, publishedEntry())
})

test('an installed platform package without its binary is not reported as missing', () => {
  withDir((dir) => {
    const pkgDir = join(dir, 'node_modules', `codegen-pkg-${target}`)
    mkdirSync(pkgDir, { recursive: true })
    writeFileSync(join(pkgDir, 'package.json'), JSON.stringify({ name: `codegen-pkg-${target}`, version: '1.0.0', main: 'gone.node' }))
    const r = requireIn(dir)
    assert.equal(r.ok, false)
    assert.doesNotMatch(r.message, /package not installed/)
    assert.match(r.message, /gone\.node/)
  }, publishedEntry())
})

test('on musl the error says that musl is not supported', () => {
  withDir((dir) => {
    // Pretend to be a Linux machine whose ldd is the one of musl, before the loader runs.
    const preload = join(dir, 'musl.cjs')
    writeFileSync(preload, `
      const fs = require('node:fs')
      const readFileSync = fs.readFileSync
      fs.readFileSync = (path, ...rest) => path === '/usr/bin/ldd' ? 'exec /lib/ld-musl-x86_64.so.1 --list "$@"' : readFileSync(path, ...rest)
      Object.defineProperty(process, 'platform', { value: 'linux' })`)
    copyFileSync(addonPath('codegen'), join(dir, `codegen.linux-${process.arch}-gnu.node`))
    const r = requireIn(dir, { nodeArgs: ['--require', preload] })
    assert.equal(r.ok, false)
    assert.match(r.message, new RegExp(`for linux-${process.arch}-musl \\(musl libc, as on Alpine Linux, is not supported`))
  })
})

test('ES modules import the exports by name', () => {
  withDir((dir) => {
    copyFileSync(addonPath('codegen'), join(dir, 'codegen.node'))
    const url = JSON.stringify(pathToFileURL(join(dir, 'index.js')).href)
    const r = nodeEval(`import(${url}).then(({ add, Cart, MAX_ITEMS, default: all }) => process.stdout.write(JSON.stringify({ sum: add(2, 3), cart: typeof Cart, max: MAX_ITEMS, same: all.add === add }) + '\\n'))`)
    assert.equal(r.status, 0, r.stderr)
    assert.deepEqual(lastJSONLine(r), { sum: 5, cart: 'function', max: 3, same: true })
  })
})
