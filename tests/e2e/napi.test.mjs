// End-to-end tests for Layer 1 (package napi) through the tests/e2e/addons/napibasic addon.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Worker } from 'node:worker_threads'
import { load, addonPath, isDarwinX64, nodeEval, lastJSONLine, hostNapiVersion } from './helpers.mjs'

const A = load('napibasic')

const INLINE_ARGS = 8 // GN_INLINE_ARGS in sys/internal.h: arguments read by the C trampoline
const MANY_ARGS = 20
const STACK_STRING_BYTES = 256 // stackStringBytes in napi/primitive.go
const UTF8_MAX_CHAR_BYTES = 4
const LONG_STRING_LENGTH = 100_000
const BYTE_MODULUS = 251 // byteModulus in the addon
const BUFFER_SIZE = 1000
const GC_OBJECTS = 2000
const ALLOC_CALLS = 20_000
const MAX_ALLOCS_PER_CALL = 0.01
const V9 = 9
const V10 = 10
const INT64_MAX = 2n ** 63n - 1n
const INT64_MIN = -(2n ** 63n)
const UINT32_WRAP = 2 ** 32

const sumPattern = (n) => {
  let s = 0
  for (let i = 0; i < n; i++) s += i % BYTE_MODULUS
  return s
}

// expectError verifies the name, code, and (optional) message of an error thrown by fn.
function expectError (fn, name, code, message) {
  assert.throws(fn, (e) => {
    assert.equal(e.name, name, `name of ${e}`)
    assert.equal(e.code, code, `code of ${e}`)
    if (message !== undefined) assert.equal(e.message, message)
    return true
  })
}

test('init: multiple init functions in order, exports materialized per env', () => {
  assert.equal(A.hello(), 'world')
  assert.equal(A.secondInitSawExports, true)
  assert.equal(typeof A.Counter, 'function')
  assert.equal(A.hello.name, 'hello')
  if (hostNapiVersion >= V10) {
    // Two builds of the same addon are separate modules with their own state (in a separate
    // process: on macOS x64 a second Go runtime in the same process is rejected).
    const r = nodeEval(`const a = require(${JSON.stringify(addonPath('napibasic'))});
      let second;
      try {
        const b = require(${JSON.stringify(addonPath('napibasic-v10'))});
        second = { hello: b.hello(), distinct: a.hello !== b.hello };
      } catch (e) {
        second = { code: e.code };
      }
      process.stdout.write(JSON.stringify(second))`)
    assert.equal(r.status, 0, r.stderr)
    if (isDarwinX64) {
      assert.deepEqual(lastJSONLine(r), { code: 'ERR_NAPI_GO_FOREIGN_RUNTIME' })
    } else {
      assert.deepEqual(lastJSONLine(r), { hello: 'world', distinct: true })
    }
  }
})

test('arguments: none, beyond inline buffer, this, missing', () => {
  const none = A.argInfo()
  assert.equal(none.len, 0)
  assert.equal(none.missing, undefined)
  assert.equal(none.negative, undefined)
  assert.equal(none.last, null)
  assert.equal(none.isConstruct, false)
  const args = Array.from({ length: MANY_ARGS }, (_, i) => i)
  const many = A.argInfo(...args)
  assert.equal(many.len, MANY_ARGS)
  assert.equal(many.last, MANY_ARGS - 1)
  const boundary = A.argInfo(...args.slice(0, INLINE_ARGS))
  assert.equal(boundary.last, INLINE_ARGS - 1)
  const self = { tag: 1 }
  assert.equal(A.argInfo.call(self, 1).this, self)
  assert.equal(new A.argInfo().isConstruct, true)
  assert.equal(A.echo(undefined), undefined)
  const o = {}
  assert.equal(A.echo(o), o)
})

test('typeof and predicates for every type', () => {
  const cases = [
    [undefined, 'undefined'], [null, 'null'], [true, 'boolean'], [1, 'number'], ['s', 'string'],
    [Symbol('s'), 'symbol'], [{}, 'object'], [() => 1, 'function'], [1n, 'bigint'],
    [A.external('new', 'x'), 'external'],
  ]
  for (const [v, type] of cases) assert.equal(A.typeOf(v).type, type, String(type))
  const t = (v) => A.typeOf(v)
  assert.equal(t(null).isNullish, true)
  assert.equal(t(undefined).isNullish, true)
  assert.equal(t(0).isNullish, false)
  assert.equal(t(() => 1).isObject, true)
  assert.equal(t(null).isObject, false)
  assert.equal(t([]).isArray, true)
  assert.equal(t(new ArrayBuffer(1)).isArrayBuffer, true)
  assert.equal(t(Buffer.alloc(1)).isBuffer, true)
  assert.equal(t(new Uint8Array(1)).isTypedArray, true)
  assert.equal(t(new DataView(new ArrayBuffer(1))).isDataView, true)
  assert.equal(t(new Date()).isDate, true)
  assert.equal(t(Promise.resolve()).isPromise, true)
  assert.equal(t(new RangeError()).isError, true)
  assert.equal(t({}).isError, false)
})

test('numbers: conversions and boundary values', () => {
  assert.equal(A.add(0.1, 0.2), 0.1 + 0.2)
  for (const x of [0, 1, -1, 2 ** 31, -(2 ** 31) - 1, UINT32_WRAP + 5, -1.7, 2 ** 53 + 2, 1e-300]) {
    const r = A.numbers(x)
    assert.equal(r.float64, x)
    assert.equal(r.int32, x | 0, `int32 of ${x}`)
    assert.equal(r.uint32, x >>> 0, `uint32 of ${x}`)
    assert.equal(r.int64big, BigInt(Math.trunc(x)), `int64 of ${x}`)
  }
  // -0 is preserved as float64.
  assert.ok(Object.is(A.numbers(-0).float64, -0))
  // Non-finite values: 0 in integer types; out-of-range int64 saturates.
  for (const x of [NaN, Infinity, -Infinity]) {
    const r = A.numbers(x)
    assert.ok(Object.is(r.float64, x) || (Number.isNaN(x) && Number.isNaN(r.float64)))
    assert.equal(r.int32, 0)
    assert.equal(r.uint32, 0)
    assert.equal(r.int64big, 0n)
  }
  assert.equal(A.numbers(1e300).int64big, INT64_MAX)
  assert.equal(A.numbers(-1e300).int64big, INT64_MIN)
  expectError(() => A.add('x', 1), 'TypeError', 'ERR_NAPI_NUMBER_EXPECTED')
  expectError(() => A.numbers(1n), 'TypeError', 'ERR_NAPI_NUMBER_EXPECTED')
  assert.equal(A.bool(false), true)
  expectError(() => A.bool(0), 'TypeError', 'ERR_NAPI_BOOLEAN_EXPECTED')
})

test('strings: empty, UTF-8, stack buffer boundary, long, UTF-16, Latin-1', () => {
  assert.equal(A.str(''), '')
  assert.equal(A.str('hello, 世界 😀'), 'hello, 世界 😀')
  // Around the stack buffer threshold, with 1-, 2-, 3-, and 4-byte characters.
  const limit = STACK_STRING_BYTES - 1 - UTF8_MAX_CHAR_BYTES
  for (const ch of ['a', 'é', '世', '😀']) {
    const bytes = Buffer.byteLength(ch)
    for (let n = Math.floor((limit - UTF8_MAX_CHAR_BYTES) / bytes); n * bytes <= STACK_STRING_BYTES + UTF8_MAX_CHAR_BYTES; n++) {
      const s = ch.repeat(n)
      assert.equal(A.str(s), s, `${n} × ${ch}`)
      assert.equal(A.strBytes(s), n * bytes)
    }
  }
  const long = 'x'.repeat(LONG_STRING_LENGTH) + '😀'
  assert.equal(A.str(long), long)
  // An unpaired surrogate is not valid UTF-8: it becomes U+FFFD; in UTF-16 it is preserved as-is.
  assert.equal(A.str('\uD800x'), '�x')
  assert.equal(A.utf16('\uD800x'), '\uD800x')
  assert.equal(A.utf16(''), '')
  assert.equal(A.utf16('😀 ok'), '😀 ok')
  // Latin-1: characters above U+00FF are truncated to the low byte.
  assert.equal(A.latin1('café'), 'café')
  assert.equal(A.latin1('Ā'), '\u0000')
  assert.equal(A.latin1(''), '')
  expectError(() => A.str(1), 'TypeError', 'ERR_NAPI_STRING_EXPECTED')
  expectError(() => A.utf16(null), 'TypeError', 'ERR_NAPI_STRING_EXPECTED')
})

test('symbol: description, uniqueness, global registry', () => {
  const s = A.symbol('desc')
  assert.equal(typeof s, 'symbol')
  assert.equal(s.description, 'desc')
  assert.notEqual(A.symbol('desc'), s)
  assert.equal(A.symbol('', false).description, '')
  if (hostNapiVersion >= V9) assert.equal(A.symbol('k', true), Symbol.for('k'))
})

test('bigint: int64, uint64, words, *big.Int', () => {
  assert.deepEqual(A.bigint(-5n, 'i64'), [-5n, true])
  assert.deepEqual(A.bigint(INT64_MIN, 'i64'), [INT64_MIN, true])
  assert.deepEqual(A.bigint(INT64_MAX + 1n, 'i64'), [INT64_MIN, false])
  assert.deepEqual(A.bigint(2n ** 64n, 'i64'), [0n, false])
  assert.deepEqual(A.bigint(2n ** 64n - 1n, 'u64'), [2n ** 64n - 1n, true])
  assert.deepEqual(A.bigint(-1n, 'u64'), [2n ** 64n - 1n, false])
  for (const b of [0n, 1n, -1n, 2n ** 64n, -(2n ** 130n) - 7n, 12345678901234567890123456789n]) {
    assert.equal(A.bigint(b, 'big'), b)
  }
  assert.deepEqual(A.bigint(0n, 'words'), [0, false])
  assert.deepEqual(A.bigint(-(2n ** 64n), 'words'), [2, true])
  expectError(() => A.bigint(1, 'i64'), 'TypeError', 'ERR_NAPI_BIGINT_EXPECTED')
})

test('Date: round-trip, milliseconds, invalid Date', () => {
  // Even outside the range of time.UnixNano (years 1678-2262).
  const YEAR_1 = -62_135_596_800_000
  const YEAR_9999 = 253_402_300_799_999
  for (const ms of [0, -1, 1_700_000_000_123, YEAR_1, YEAR_9999]) {
    assert.equal(A.date(new Date(ms)).getTime(), ms)
  }
  // A Date time value is an integer (JS TimeClip).
  const FRACTION = 0.75
  assert.equal(A.date(1e12 + FRACTION, true).getTime(), 1e12)
  assert.ok(Number.isNaN(A.date(NaN, true).getTime()))
  expectError(() => A.date(new Date(NaN)), 'RangeError', undefined, 'invalid Date')
  expectError(() => A.date({}), 'TypeError', 'ERR_NAPI_DATE_EXPECTED')
})

test('properties by string, value, and index key', () => {
  const o = { a: 1, '': 'empty', 'è ü': 'unicode' }
  assert.equal(A.objectOps(o, 'get', 'a'), 1)
  assert.equal(A.objectOps(o, 'get', ''), 'empty')
  assert.equal(A.objectOps(o, 'get', 'è ü'), 'unicode')
  assert.equal(A.objectOps(o, 'get', 'missing'), undefined)
  A.objectOps(o, 'set', 'b', 2)
  assert.equal(o.b, 2)
  assert.equal(A.objectOps(o, 'has', 'toString'), true)
  assert.equal(A.objectOps(o, 'hasOwn', 'toString'), false)
  assert.equal(A.objectOps(o, 'delete', 'b'), true)
  assert.equal('b' in o, false)
  const sym = Symbol('k')
  A.objectOps(o, 'setV', sym, 'symval')
  assert.equal(o[sym], 'symval')
  assert.equal(A.objectOps(o, 'getV', sym), 'symval')
  assert.equal(A.objectOps(o, 'hasOwnV', sym), true)
  assert.equal(A.objectOps(o, 'deleteV', sym), true)
  assert.equal(A.objectOps(o, 'hasV', sym), false)
  const arr = [10, 20]
  assert.equal(A.objectOps(arr, 'getI', 1), 20)
  A.objectOps(arr, 'setI', 5, 60)
  assert.equal(arr.length, 6)
  assert.equal(A.objectOps(arr, 'hasI', 3), false)
  assert.equal(A.objectOps(arr, 'deleteI', 0), true)
  assert.equal(0 in arr, false)
  // Non-configurable property: delete returns false; frozen: set changes nothing.
  const frozen = Object.freeze({ k: 1 })
  assert.equal(A.objectOps(frozen, 'delete', 'k'), false)
  A.objectOps(frozen, 'set', 'k', 2)
  assert.equal(frozen.k, 1)
  // Throwing getter: the JS exception passes through Go unchanged.
  const boom = new Error('getter')
  assert.throws(() => A.objectOps({ get x () { throw boom } }, 'get', 'x'), (e) => e === boom)
  // Proxy: traps are executed.
  const seen = []
  const proxy = new Proxy({}, { get: (_, k) => { seen.push(k); return 7 } })
  assert.equal(A.objectOps(proxy, 'get', 'p'), 7)
  assert.deepEqual(seen, ['p'])
  expectError(() => A.objectOps(1, 'get', 'a'), 'TypeError', 'ERR_NAPI_OBJECT_EXPECTED', 'expected an object, got number')
  expectError(() => A.objectOps(null, 'get', 'a'), 'TypeError', 'ERR_NAPI_OBJECT_EXPECTED', 'expected an object, got null')
})

test('keys, prototype, freeze, and seal', () => {
  const proto = { inherited: 1 }
  const o = Object.create(proto)
  o.own = 2
  o[3] = 'idx'
  Object.defineProperty(o, 'hidden', { value: 1, enumerable: false })
  const sym = Symbol('s')
  o[sym] = 1
  assert.deepEqual(A.keys(o, 'names').sort(), ['3', 'inherited', 'own'])
  assert.deepEqual(A.keys(o, 'own'), Object.keys(o))
  assert.deepEqual(A.keys(o, 'all'), Reflect.ownKeys(o).map((k) => (k === '3' ? 3 : k)))
  assert.equal(A.keys(o, 'proto'), proto)
  const f = A.freeze({ a: 1 })
  assert.ok(Object.isFrozen(f))
  const s = A.freeze({ a: 1 }, true)
  assert.ok(Object.isSealed(s) && !Object.isFrozen(s))
})

test('arrays: creation, length, holes', () => {
  assert.deepEqual(A.array(), [])
  assert.deepEqual(A.array(1, 'a', null), [1, 'a', null])
  assert.equal(A.arrayLen([1, 2, 3]), 3)
  assert.equal(A.arrayLen(new Array(5)), 5)
  expectError(() => A.arrayLen({ length: 1 }), 'TypeError', 'ERR_NAPI_ARRAY_EXPECTED')
})

test('calling JS functions: this, arguments, exceptions', () => {
  const self = { k: 1 }
  assert.equal(A.call(function () { return this }, self), self)
  // this is not boxed: undefined stays undefined for a strict function (this file is an ES
  // module) and becomes globalThis for a sloppy one.
  assert.equal(A.call(function () { return this }, undefined), undefined)
  assert.equal(A.call(new Function('return this'), undefined), globalThis)
  assert.equal(A.call((...a) => a.length, null), 0)
  const args = Array.from({ length: MANY_ARGS }, (_, i) => i)
  assert.deepEqual(A.call((...a) => a, null, ...args), args)
  const err = new TypeError('from js')
  assert.throws(() => A.call(() => { throw err }, null), (e) => e === err)
  // A non-Error value thrown from JS arrives as-is.
  assert.throws(() => A.call(() => { throw 42 }, null), (e) => e === 42)
  expectError(() => A.call({}, null), 'TypeError', 'ERR_NAPI_FUNCTION_EXPECTED')
  class Point { constructor (x, y) { this.x = x; this.y = y } }
  const p = A.construct(Point, 1, 2)
  assert.ok(p instanceof Point)
  assert.deepEqual({ ...p }, { x: 1, y: 2 })
  assert.throws(() => A.construct(() => 1), TypeError)
})

test('Catch: intercepts and clears the exception; without Catch it propagates', () => {
  const err = new Error('caught')
  const r = A.catch(() => { throw err })
  assert.equal(r.caught, err)
  assert.equal(r.pendingAfterCatch, false)
  assert.deepEqual(A.catch((x) => x * 2, 21), { value: 42 })
  assert.throws(() => A.catch.call('propagate', () => { throw err }), (e) => e === err)
})

test('runtime-created functions: name, arguments, new', () => {
  const f = A.makeFunction('dyn')
  assert.equal(f.name, 'dyn')
  const self = {}
  const [argc, thisArg, isNew] = f.call(self, 1, 2, 3)
  assert.equal(argc, 3)
  assert.equal(thisArg, self)
  assert.equal(isNew, false)
  assert.equal(new f()[2], true)
  assert.equal(A.makeFunction('').name, '')
})

test('Go errors → JS exceptions: type, code, message', () => {
  expectError(() => A.throwError('error', 'E_ONE', 'one'), 'Error', 'E_ONE', 'one')
  expectError(() => A.throwError('type', 'E_TYPE', 'bad type'), 'TypeError', 'E_TYPE', 'bad type')
  expectError(() => A.throwError('range', '', 'out of range'), 'RangeError', undefined, 'out of range')
  expectError(() => A.throwError('syntax', 'E_SYN', 'syn'), 'SyntaxError', 'E_SYN', 'syn')
  expectError(() => A.throwError('plain', '', 'plain error'), 'Error', undefined, 'plain error')
  expectError(() => A.throwError('coder', 'E_CODER', 'with code'), 'Error', 'E_CODER', 'with code')
  expectError(() => A.throwError('wrapped', 'E_WRAP', 'wrapped'), 'RangeError', 'E_WRAP', 'wrapped')
  expectError(() => A.throwError('napi'), 'TypeError', 'ERR_NAPI_NUMBER_EXPECTED', 'A number was expected')
  // An already pending exception is not replaced by the returned error.
  expectError(() => A.throwError('pending'), 'TypeError', 'FIRST', 'first')
  // Throw throws any value.
  for (const v of [42, 'str', null, { o: 1 }]) {
    assert.throws(() => A.throwError('value', v), (e) => e === v)
  }
  const e = A.throwError('newError', 'E_NEW', 'not thrown')
  assert.ok(e instanceof RangeError)
  assert.equal(e.code, 'E_NEW')
  // A panic becomes an Error with code GO_PANIC and the process survives.
  expectError(() => A.panic(), 'Error', 'GO_PANIC')
  assert.equal(A.hello(), 'world')
})

test('coercion: String, Number, Boolean, Object (running JS)', () => {
  assert.equal(A.coerce(123, 'string'), '123')
  assert.equal(A.coerce('42', 'number'), 42)
  assert.equal(A.coerce(0, 'bool'), false)
  assert.equal(A.coerce('x', 'bool'), true)
  assert.ok(A.coerce(1, 'object') instanceof Number)
  let calls = 0
  const v = { valueOf () { calls++; return 9 } }
  assert.equal(A.coerce(v, 'number'), 9)
  assert.equal(calls, 1)
  assert.throws(() => A.coerce(Symbol('s'), 'string'), TypeError)
  assert.throws(() => A.coerce(null, 'object'), TypeError)
  assert.throws(() => A.coerce(1n, 'number'), TypeError)
})

test('comparisons: === and instanceof', () => {
  const o = {}
  assert.deepEqual(A.compare(o, o), [true, false])
  assert.deepEqual(A.compare(NaN, NaN), [false, false])
  assert.deepEqual(A.compare(1, '1'), [false, false])
  assert.deepEqual(A.compare([], null, Array), [false, true])
  assert.deepEqual(A.compare({}, null, Array), [false, false])
  // Symbol.hasInstance is executed.
  const Always = { [Symbol.hasInstance]: () => true }
  assert.throws(() => A.compare(1, 1, Always), TypeError) // not a function
  function F () {}
  Object.defineProperty(F, Symbol.hasInstance, { value: () => true })
  assert.deepEqual(A.compare(1, 1, F), [true, true])
})

test('DefineProperties: methods, accessors, values, attributes, symbol keys', () => {
  const sym = Symbol('answer')
  const o = A.defineProps({}, sym)
  assert.equal(o.twice(21), 42)
  o.acc = 5
  assert.equal(o.acc, 5)
  assert.equal(o.readOnly, 5)
  o.writeOnly = 7
  assert.equal(o.acc, 7)
  assert.equal(o.writeOnly, undefined)
  assert.equal(o.answer, 42)
  assert.equal(o[sym], 42)
  assert.equal(o.hidden, 'hidden')
  assert.deepEqual(Object.keys(o).sort(), ['acc', 'answer', 'readOnly'])
  const d = (k) => Object.getOwnPropertyDescriptor(o, k)
  assert.deepEqual([d('twice').writable, d('twice').enumerable, d('twice').configurable], [true, false, true])
  assert.deepEqual([d('acc').enumerable, d('acc').configurable], [true, true])
  assert.deepEqual([d('answer').writable, d('answer').configurable], [false, false])
  assert.equal(d('hidden').writable, true)
  // Property without a setter or non-writable in strict mode: TypeError.
  assert.throws(() => { 'use strict'; o.readOnly = 1 }, TypeError)
  assert.throws(() => { 'use strict'; o.answer = 1 }, TypeError)
})

test('classes: constructor, methods, accessors, statics, subclasses', () => {
  const c = new A.Counter(5)
  assert.ok(c instanceof A.Counter)
  assert.equal(c.inc(), 6)
  assert.equal(c.inc(4), 10)
  assert.equal(c.value, 10)
  c.value = -1
  assert.equal(c.value, -1)
  assert.equal(new A.Counter().value, 0)
  assert.equal(A.Counter.kind, 'counter')
  assert.equal(A.Counter.name, 'Counter')
  assert.equal(A.Counter.isCounter(c), true)
  assert.equal(A.Counter.isCounter({}), false)
  assert.equal(A.Counter.isCounter(1), false)
  assert.deepEqual(Object.getOwnPropertyNames(A.Counter.prototype).sort(), ['constructor', 'inc', 'value'])
  expectError(() => A.Counter(1), 'TypeError', undefined, "Class constructor Counter cannot be invoked without 'new'")
  // Calling a method with a non-Counter this: V8 rejects it before reaching Go.
  assert.throws(() => A.Counter.prototype.inc.call({}), TypeError)
  class Sub extends A.Counter {
    double () { return this.inc(this.value) }
  }
  const s = new Sub(3)
  assert.ok(s instanceof Sub && s instanceof A.Counter)
  assert.equal(s.double(), 6)
  expectError(() => new A.Counter('x'), 'TypeError', 'ERR_NAPI_NUMBER_EXPECTED')
})

test('Wrap: attach, read, remove, wrong types', () => {
  const o = {}
  A.wrap(o, 'wrap', 1.5)
  assert.equal(A.wrap(o, 'unwrap'), 1.5)
  expectError(() => A.wrap(o, 'wrap', 2), 'Error', 'ERR_NAPI_INVALID_ARG')
  expectError(() => A.wrap(o, 'unwrapWrongType'), 'TypeError', undefined, 'wrapped Go value has a different type')
  assert.equal(A.wrap(o, 'remove'), 1.5)
  expectError(() => A.wrap(o, 'unwrap'), 'TypeError', 'ERR_NAPI_INVALID_ARG', 'object does not wrap a Go value')
  expectError(() => A.wrap(o, 'remove'), 'TypeError', 'ERR_NAPI_INVALID_ARG')
  // After RemoveWrap a new value can be attached.
  A.wrap(o, 'wrap', 3)
  assert.equal(A.wrap(o, 'unwrap'), 3)
  expectError(() => A.wrap(1, 'wrap', 1), 'TypeError', 'ERR_NAPI_OBJECT_EXPECTED')
})

test('External: opaque Go value', () => {
  const x = A.external('new', 'payload')
  assert.equal(typeof x, 'object')
  assert.equal(A.external('get', x), 'payload')
  assert.equal(Object.getPrototypeOf(x), null)
  expectError(() => A.external('get', {}), 'TypeError', 'ERR_NAPI_INVALID_ARG', 'expected an external, got object')
})

test('Ref: strong, refcount, primitives by version', () => {
  const o = { keep: true }
  const id = A.refs('new', o, 1)
  assert.equal(A.refs('get', id), o)
  assert.equal(A.refs('ref', id), 2)
  assert.equal(A.refs('unref', id), 1)
  assert.equal(A.refs('unref', id), 0)
  // At refcount 0 the reference is weak, but the object is still reachable from here.
  assert.equal(A.refs('get', id), o)
  A.refs('delete', id)
  expectError(() => A.refs('get', id), 'RangeError', undefined, 'unknown ref')
  // Node-API 8: objects and symbols only; Node-API 10: any value (in a separate process:
  // two Go addons in the same process are not supported on macOS x64).
  expectError(() => A.refs('new', 5, 1), 'Error', 'ERR_NAPI_INVALID_ARG')
  if (hostNapiVersion >= V10) {
    const r = nodeEval(`const b = require(${JSON.stringify(addonPath('napibasic-v10'))});
      const pid = b.refs('new', 5, 1);
      const got = b.refs('get', pid);
      b.refs('delete', pid);
      process.stdout.write(JSON.stringify({ got }))`)
    assert.equal(r.status, 0, r.stderr)
    assert.deepEqual(lastJSONLine(r), { got: 5 })
  }
})

test('weak Ref: value collected by GC becomes empty', () => {
  const r = nodeEval(`const a = require(${JSON.stringify(addonPath('napibasic'))});
    let id = a.refs('new', { big: new Array(1000).fill(0) }, 0);
    gc();
    setImmediate(() => { gc(); process.stdout.write(JSON.stringify({ v: a.refs('get', id) })) })`,
  { nodeArgs: ['--expose-gc'] })
  assert.equal(r.status, 0, r.stderr)
  assert.deepEqual(lastJSONLine(r), { v: '<collected>' })
})

test('Buffer and ArrayBuffer: V8 memory, copies, zero-copy externals, detach', () => {
  const expected = sumPattern(BUFFER_SIZE)
  for (const op of ['new', 'copy', 'external']) {
    const b = A.buffers(op, BUFFER_SIZE)
    assert.ok(Buffer.isBuffer(b), op)
    assert.equal(b.length, BUFFER_SIZE)
    assert.equal(b[BYTE_MODULUS + 1], 1, op)
    assert.equal(A.buffers('sum', b), expected, op)
  }
  assert.equal(A.buffers('alias', 4)[0], 255)
  assert.equal(A.buffers('new', 0).length, 0)
  assert.equal(A.buffers('copy', 0).length, 0)
  assert.equal(A.buffers('externalEmpty').length, 0)
  // Go reads memory written by JS.
  const js = Buffer.from([1, 2, 3])
  assert.equal(A.buffers('sum', js), 6)
  assert.equal(A.buffers('sum', new Uint8Array([4, 5])), 9)
  for (const op of ['arrayBuffer', 'externalArrayBuffer']) {
    const ab = A.buffers(op, BUFFER_SIZE)
    assert.ok(ab instanceof ArrayBuffer, op)
    assert.equal(ab.byteLength, BUFFER_SIZE)
    assert.equal(A.buffers('abSum', ab), expected)
    assert.equal(new Uint8Array(ab)[BYTE_MODULUS], 0)
  }
  const ab = A.buffers('arrayBuffer', BUFFER_SIZE)
  assert.equal(A.buffers('isDetached', ab), false)
  A.buffers('detach', ab)
  assert.equal(A.buffers('isDetached', ab), true)
  assert.equal(ab.byteLength, 0)
  assert.equal(A.buffers('abSum', ab), 0)
  expectError(() => A.buffers('new', -1), 'RangeError', undefined, 'negative buffer size')
  expectError(() => A.buffers('sum', {}), 'TypeError', 'ERR_NAPI_INVALID_ARG', 'expected a Buffer, got object')
  expectError(() => A.buffers('abSum', Buffer.alloc(1)), 'TypeError', 'ERR_NAPI_ARRAYBUFFER_EXPECTED')
})

test('TypedArray and DataView: creation, info, writing from Go memory', () => {
  const INT16 = 3
  const BIGINT64 = 9
  const ab = new ArrayBuffer(16)
  const ta = A.typed('typed', INT16, ab, 2, 3)
  assert.ok(ta instanceof Int16Array)
  assert.equal(ta.length, 3)
  assert.equal(ta.byteOffset, 2)
  const info = A.typed('info', ta)
  assert.deepEqual({ ...info, buffer: undefined }, { type: INT16, length: 3, byteOffset: 2, bytes: 6, elementSize: 2, buffer: undefined })
  assert.equal(info.buffer, ab)
  A.typed('fillTyped', new Uint8Array(ab, 4, 4))
  assert.deepEqual([...new Uint8Array(ab, 4, 4)], [0, 1, 2, 3])
  const big = A.typed('typed', BIGINT64, new ArrayBuffer(16), 0, 2)
  assert.ok(big instanceof BigInt64Array)
  assert.equal(A.typed('info', big).bytes, 16)
  assert.throws(() => A.typed('typed', INT16, ab, 1, 2), (e) => e instanceof RangeError && e.code === 'ERR_NAPI_INVALID_TYPEDARRAY_ALIGNMENT')
  assert.throws(() => A.typed('typed', INT16, ab, 2, 100), RangeError)
  const dv = A.typed('dataView', 0, ab, 4, 8)
  assert.ok(dv instanceof DataView)
  const dvi = A.typed('dvInfo', dv)
  assert.equal(dvi.byteOffset, 4)
  assert.equal(dvi.bytes, 8)
  assert.equal(dvi.buffer, ab)
  assert.throws(() => A.typed('dataView', 0, ab, 10, 10), RangeError)
  expectError(() => A.typed('info', []), 'TypeError', 'ERR_NAPI_INVALID_ARG', 'expected a TypedArray, got object')
})

test('Promises resolved and rejected on the JS thread', async () => {
  const o = {}
  assert.equal(await A.promise('resolve', o), o)
  await assert.rejects(A.promise('reject', 'nope'), (e) => e === 'nope')
  await assert.rejects(A.promise('error'), (e) => e instanceof RangeError && e.code === 'E_PROMISE' && e.message === 'rejected from Go')
  await assert.rejects(A.promise('thrown', 7), (e) => e === 7)
  expectError(() => A.promise('other'), 'RangeError', undefined, 'unknown op other')
})

test('handle scopes and escapable scopes', () => {
  const SCOPE_ITERATIONS = 10_000
  assert.deepEqual({ ...A.scopes(SCOPE_ITERATIONS) }, { n: SCOPE_ITERATIONS })
  assert.deepEqual({ ...A.scopes(0) }, { n: 0 })
})

test('RunScript, versions, module file name', () => {
  assert.equal(A.runScript('1 + 2'), 3)
  assert.equal(A.runScript('globalThis'), globalThis)
  assert.throws(() => A.runScript('1 +'), SyntaxError)
  assert.throws(() => A.runScript('throw new RangeError("rs")'), RangeError)
  const v = A.versions()
  assert.equal(v.napi, hostNapiVersion)
  assert.equal(v.node, process.versions.node.replace(/-.*$/, ''))
  assert.equal(v.release, 'node')
  if (hostNapiVersion >= V9) assert.ok(v.file.startsWith('file://') && v.file.endsWith('napibasic.node'), v.file)
})

test('instance data per env and worker: isolated state, cleanup hook and finalizer on exit', async () => {
  A.instanceData(1)
  assert.equal(A.instanceData(), 1)
  const before = A.counters()
  const worker = new Worker(`
    const { parentPort } = require('node:worker_threads')
    const a = require(${JSON.stringify(addonPath('napibasic'))})
    const fresh = a.instanceData()
    a.instanceData(2)
    a.cleanupHook()
    parentPort.postMessage({ fresh, set: a.instanceData(), envs: a.counters().envs })`, { eval: true })
  // Attach both listeners before any await: 'exit' may fire right after 'message'.
  const exited = new Promise((resolve) => worker.once('exit', resolve))
  const msg = await new Promise((resolve, reject) => {
    worker.once('message', resolve)
    worker.once('error', reject)
  })
  await exited
  assert.deepEqual(msg, { fresh: 0, set: 2, envs: before.envs + 1 })
  // Main thread value is unchanged.
  assert.equal(A.instanceData(), 1)
  const after = A.counters()
  assert.equal(after.cleanupHooksRun - before.cleanupHooksRun, 1)
  assert.equal(after.instanceFinalized - before.instanceFinalized, 1)
  assert.equal(after.envs, before.envs)
  assert.equal(after.inits - before.inits, 1)
})

test('GC: callbacks, wrapped values, externals, and external buffers are released', () => {
  const r = nodeEval(`const a = require(${JSON.stringify(addonPath('napibasic'))});
    const base = a.counters();
    (function () {
      for (let i = 0; i < ${GC_OBJECTS}; i++) a.makeFunction('f' + i);
      a.wrap(null, 'many', ${GC_OBJECTS});
      a.external('many', ${GC_OBJECTS});
      for (let i = 0; i < ${GC_OBJECTS}; i++) a.buffers('external', 16);
      for (let i = 0; i < ${GC_OBJECTS}; i++) a.defineProps({});
      for (let i = 0; i < ${GC_OBJECTS}; i++) new a.Counter(i);
    })();
    const peak = a.counters();
    const deadline = Date.now() + 20000;
    (function poll () {
      gc();
      const c = a.counters();
      const done = c.callbacks === base.callbacks && c.handles === base.handles;
      if (done || Date.now() > deadline) {
        process.stdout.write(JSON.stringify({ base, peak, end: c }));
        return;
      }
      setImmediate(poll);
    })()`, { nodeArgs: ['--expose-gc'] })
  assert.equal(r.status, 0, r.stderr)
  const { base, peak, end } = lastJSONLine(r)
  assert.ok(peak.callbacks > base.callbacks + GC_OBJECTS, 'registered callbacks')
  assert.ok(peak.handles > base.handles + GC_OBJECTS, 'registered handles')
  assert.equal(end.callbacks, base.callbacks, 'released callbacks')
  assert.equal(end.handles, base.handles, 'released handles')
  // Counter shares the same counter as wraps.
  assert.equal(end.wrapFinalized - base.wrapFinalized, 2 * GC_OBJECTS)
  assert.equal(end.externalFinalized - base.externalFinalized, GC_OBJECTS)
  assert.equal(end.bufferFinalized - base.bufferFinalized, GC_OBJECTS)
})

test('zero Go allocations per call on common hot paths', () => {
  const measure = (fn) => {
    fn() // warm-up
    const before = A.goMallocs()
    for (let i = 0; i < ALLOC_CALLS; i++) fn()
    return (A.goMallocs() - before) / ALLOC_CALLS
  }
  const obj = { k: 1 }
  assert.ok(measure(() => A.noop()) < MAX_ALLOCS_PER_CALL, 'noop')
  assert.ok(measure(() => A.noop(1, 2, 3)) < MAX_ALLOCS_PER_CALL, 'noop with arguments')
  assert.ok(measure(() => A.hotPath(obj, '')) < MAX_ALLOCS_PER_CALL, 'hotPath')
  const zero = () => 0
  assert.ok(measure(() => A.callBack(zero, false)) < MAX_ALLOCS_PER_CALL, 'Function.Call without arguments')
  // A non-empty string allocates only the returned Go string; Call with arguments allocates only
  // their buffer.
  const ONE_ALLOC = 1
  assert.ok(measure(() => A.hotPath(obj, 'hello')) <= ONE_ALLOC + MAX_ALLOCS_PER_CALL, 'hotPath with string')
  assert.ok(measure(() => A.callBack(zero, true)) <= ONE_ALLOC + MAX_ALLOCS_PER_CALL, 'Function.Call with arguments')
})

test('process exits cleanly on its own with the addon loaded', () => {
  const r = nodeEval(`require(${JSON.stringify(addonPath('napibasic'))}).hello()`)
  assert.equal(r.status, 0, r.stderr)
  assert.equal(r.signal, null)
})
