// End-to-end tests of Layer 2 (napi.Export, generic wrappers, conversions, async, classes,
// JSFunc) on the addon tests/e2e/addons/framework.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { Worker } from 'node:worker_threads'
import { load, addonPath, nodeEval, lastJSONLine } from './helpers.mjs'

const A = load('framework')
const ADDON = JSON.stringify(addonPath('framework'))

const INVALID_TYPE = { name: 'TypeError', code: 'ERR_INVALID_ARG_TYPE' }
const OUT_OF_RANGE = { name: 'RangeError', code: 'ERR_OUT_OF_RANGE' }
const INVALID_VALUE = 'ERR_INVALID_ARG_VALUE'
// Size of the C buffer of the typed trampoline for strings (GN_TYPED_STRBUF).
const C_STRING_BUFFER = 512

test('numbers: conversions, integer checks and limits', () => {
  assert.equal(A.add(1.5, 2.25), 3.75)
  assert.equal(A.addInt32(2 ** 30, 2 ** 30), -(2 ** 31), 'int32 overflow wraps in Go')
  assert.equal(A.int8(-128), -128)
  assert.equal(A.int8(127), 127)
  assert.equal(A.uint8(255), 255)
  assert.equal(A.uint32(2 ** 32 - 1), 2 ** 32 - 1)
  assert.equal(A.int8(-0), 0)
  for (const bad of [128, -129, 1.5, NaN, Infinity, -Infinity]) {
    assert.throws(() => A.int8(bad), OUT_OF_RANGE, `int8(${bad})`)
  }
  assert.throws(() => A.int8(300), { message: 'argument 1: expected an integer from -128 to 127, got 300' })
  assert.throws(() => A.uint8(-1), OUT_OF_RANGE)
  assert.throws(() => A.uint32(2 ** 32), { message: /from 0 to 4294967295, got 4294967296$/ })
  assert.throws(() => A.uint64(-1), { message: /from 0 to 18446744073709551615, got -1$/ })
  assert.throws(() => A.int64(2 ** 63), { message: /-9223372036854775808 to 9223372036854775807/ })
  // 64-bit integers also accept a BigInt, if it fits.
  assert.equal(A.int64(2 ** 53), 2 ** 53)
  assert.equal(A.int64(-(2n ** 40n)), -(2 ** 40))
  assert.equal(A.uint64(2n ** 64n - 1n), '18446744073709551615')
  assert.throws(() => A.int64(2n ** 63n), OUT_OF_RANGE)
  assert.throws(() => A.uint64(-1n), OUT_OF_RANGE)
  assert.equal(A.float32(0.1), Math.fround(0.1))
  assert.equal(A.celsius(100), 212, 'named type with a numeric underlying type')
  assert.equal(A.double(2n ** 100n), 2n ** 101n, '*big.Int')
  assert.equal(A.double(3), 6n, 'an integral number converts to *big.Int')
  assert.throws(() => A.double(1.5), OUT_OF_RANGE)
  assert.throws(() => A.add('1', 2), { ...INVALID_TYPE, message: 'argument 1: expected number, got string' })
  assert.throws(() => A.add(1), { ...INVALID_TYPE, message: 'argument 2: expected number, got undefined' })
  assert.equal(A.add(1, 2, 'ignored'), 3, 'extra arguments are ignored')
})

test('strings and booleans, including the C buffer boundaries of the typed path', () => {
  assert.equal(A.greet('go'), 'hello go')
  assert.equal(A.greet(''), 'hello ')
  assert.equal(A.greet('😀ü'), 'hello 😀ü')
  assert.equal(A.not(false), true)
  assert.throws(() => A.not(1), { ...INVALID_TYPE, message: 'argument 1: expected boolean, got number' })
  assert.throws(() => A.greet(5), INVALID_TYPE)
  assert.equal(A.repeat('ab', 3), 'ababab')
  // Inputs and results around the buffer size, ending with a multi-byte character (napi does not
  // split characters, so a copy that ends near the buffer size may be incomplete).
  for (let n = C_STRING_BUFFER - 16; n <= C_STRING_BUFFER + 16; n++) {
    const s = 'a'.repeat(n - 1) + '€'
    assert.equal(A.greet(s), 'hello ' + s, `length ${n}`)
  }
  const long = 'x'.repeat(C_STRING_BUFFER * 4)
  assert.equal(A.greet(long), 'hello ' + long)
  // Three strings share the C buffer: the one that does not fit is read by Go.
  const third = 'é'.repeat(C_STRING_BUFFER / 4)
  assert.equal(A.concat3(third, third, third), [third, third, third].join('|'))
  assert.equal(A.sum8(1, 2, 3, 4, 5, 6, 7, 8), 36)
  assert.equal(A.sum8(1, 2, 3, 4, 5, 6, 7, 8, 9, 10), 36, 'arguments beyond the 8 read in C')
  assert.equal(A.argCount(1, 2, 3, 4, 5, 6, 7, 8, 9, 10), 1001)
})

test('bytes: Buffer views, copies in Async, and the accepted types', async () => {
  assert.equal(A.sumBytes(Buffer.from([1, 2, 3])), 6)
  assert.equal(A.sumBytes(new Uint8Array([4, 5])), 9)
  assert.equal(A.sumBytes(new Uint8Array([1, 2, 3, 4]).subarray(2)), 7, 'a view keeps its offset')
  assert.equal(A.sumBytes(new Uint8ClampedArray([255])), 255)
  assert.equal(A.sumBytes(new Uint8Array([1, 2]).buffer), 3, 'ArrayBuffer')
  assert.equal(A.sumBytes(new DataView(new Uint8Array([7, 8]).buffer, 1)), 8, 'DataView')
  assert.equal(A.sumBytes(Buffer.alloc(0)), 0)
  assert.throws(() => A.sumBytes(new Float64Array(2)), INVALID_TYPE)
  assert.throws(() => A.sumBytes('abc'), INVALID_TYPE)
  const out = A.upper(Buffer.from('abc'))
  assert.ok(Buffer.isBuffer(out))
  assert.equal(out.toString(), 'ABC')
  const b = Buffer.from([9, 9])
  A.zeroFirst(b)
  assert.deepEqual([...b], [0, 9], 'a []byte parameter aliases the Buffer')
  const delayMs = 20
  const later = Buffer.from([1, 2, 3])
  const p = A.sumBytesLater(later, delayMs)
  later.fill(0)
  assert.equal(await p, 6, 'Async arguments are copied before the goroutine starts')
})

test('slices, arrays and maps', () => {
  assert.equal(A.sum([1, 2, 3.5]), 6.5)
  assert.equal(A.sum([]), 0)
  assert.deepEqual(A.doubleAll([1, -2]), [2, -4])
  assert.deepEqual(A.nilSlice(), [], 'a nil slice is an empty array')
  assert.deepEqual(A.reverse3(['a', 'b', 'c']), ['c', 'b', 'a'])
  assert.throws(() => A.reverse3(['a']), { name: 'RangeError', code: INVALID_VALUE, message: 'argument 1: expected an array of length 3, got length 1' })
  assert.throws(() => A.sum({ length: 1, 0: 1 }), INVALID_TYPE, 'array-likes are not arrays')
  assert.throws(() => A.matrix([[1, 2], [3, 'x']]), { ...INVALID_TYPE, message: 'argument 1: [1][1]: expected number, got string' })
  assert.equal(A.matrix([[1, 2], [3]]), 6)
  assert.equal(A.sumValues({ a: 1, b: 2 }), 3)
  assert.equal(A.sumValues({}), 0)
  assert.throws(() => A.sumValues([1]), INVALID_TYPE, 'an array is not a map')
  assert.throws(() => A.sumValues({ a: 'x' }), { message: 'argument 1: ["a"]: expected number, got string' })
  assert.deepEqual(Object.keys(A.countWords('b a c a')), ['a', 'b', 'c'], 'keys are sorted')
  assert.deepEqual(A.countWords('b a c a'), { a: 2, b: 1, c: 1 })
  assert.deepEqual(A.invert({ 1: 'a', 20: 'b' }), { a: 1, b: 20 })
  assert.throws(() => A.invert({ x: 'a' }), { code: INVALID_VALUE, message: /\["x"\]: key is not an integer/ })
})

test('structs: names, tags, optional fields, embedding and recursion', () => {
  const u = { id: 3, name: 'Ann', age: 40, email: 'a@b.c', tags: ['x'], address: { city: 'Rome', zip: '00100' }, secret: 'no' }
  assert.deepEqual(A.echoUser(u), { id: 3, name: 'Ann', age: 40, email: 'a@b.c', tags: ['x'], address: { city: 'Rome', zip: '00100' } })
  assert.equal(A.describeUser(u), '3 Ann 40 a@b.c [x] Rome/00100 ""', 'the napi:"-" field is not read')
  const minimal = { id: 1, name: 'B', age: 2, address: { city: 'X' } }
  assert.deepEqual(A.echoUser(minimal), { id: 1, name: 'B', age: 2, email: null, address: { city: 'X' } },
    'nil pointer → null, empty omitempty fields are left out')
  assert.equal(A.describeUser({ ...minimal, email: null }), '1 B 2 <none> [] X/ ""')
  assert.throws(() => A.echoUser({ ...minimal, age: 300 }), { ...OUT_OF_RANGE, message: /^argument 1: age: / })
  assert.throws(() => A.echoUser({ ...minimal, address: { city: 1 } }), { message: 'argument 1: address.city: expected string, got number' })
  assert.throws(() => A.echoUser({ name: 'B', age: 2, address: { city: 'X' } }), { message: 'argument 1: id: expected number, got undefined' }, 'required field')
  assert.throws(() => A.echoUser(null), { message: 'argument 1: expected an object, got null' })
  assert.equal(A.sumTree({ value: 1, children: [{ value: 2 }, { value: 3, children: [{ value: 4 }] }] }), 10)
  const depth = 3
  const tree = A.makeTree(depth)
  assert.equal(A.sumTree(tree), 3 * 1 + 2 * 2 + 1 * 4 * 1 + 0, 'recursive struct round trip')
  assert.deepEqual(A.makeTree(0), { value: 0 })
})

test('pointers, any, Either, time.Time and custom conversions', () => {
  assert.equal(A.maybe(null), 'nil')
  assert.equal(A.maybe(undefined), 'nil')
  assert.equal(A.maybe(), 'nil', 'a missing argument is undefined')
  assert.equal(A.maybe(7), '7')
  assert.equal(A.maybeOut(true), 'set')
  assert.equal(A.maybeOut(false), null)
  assert.equal(A.goTypeOf({ a: [1, 'x', null, true], d: new Date(0), b: 10n, f () {}, u: new Uint8Array(2) }),
    '{a:[float64 string nil bool] b:*big.Int(10) d:time.Time f:napi.Function u:[]uint8}')
  const v = { a: [1, 'x', null, true], o: { k: -1 } }
  assert.deepEqual(A.roundTripAny(v), v)
  assert.equal(A.roundTripAny(undefined), null)
  assert.deepEqual(A.mixed(), { list: [true, null, 2.5], n: 1, nested: { a: 1 }, s: 'x' })
  assert.equal(A.stringOrBytes('hi'), 'string:hi')
  assert.equal(A.stringOrBytes(Buffer.alloc(3)), 'bytes:3')
  assert.throws(() => A.stringOrBytes(5), { ...INVALID_TYPE, message: /expected string or a Buffer/ })
  assert.equal(A.eitherOut(3), 3)
  assert.equal(A.eitherOut(-1), 'negative')
  assert.equal(A.addHour(new Date(0)).getTime(), 60 * 60 * 1000)
  assert.throws(() => A.addHour(new Date(NaN)), { name: 'RangeError', code: INVALID_VALUE })
  assert.throws(() => A.addHour(0), INVALID_TYPE)
  assert.deepEqual(A.midpoint([0, 0], [2, 4]), [1, 2], 'FromJS/ToJS')
  assert.throws(() => A.midpoint([0], [1, 1]), { code: INVALID_VALUE })
})

test('injected Env and CallContext, napi values as parameters and results', () => {
  assert.equal(A.exclaim('hey'), 'hey!')
  assert.equal(A.exclaim.length, 1, 'injected parameters are not counted in length')
  assert.equal(A.argCount(5), 105)
  assert.equal(A.argCount(5, 'x', 'y'), 305)
  assert.equal(A.callWith((x) => x * 2, 21), 42)
  assert.throws(() => A.callWith(1, 2), { ...INVALID_TYPE, message: 'argument 1: expected a function, got number' })
  assert.throws(() => A.callWith(() => { throw new Error('from js') }, 1), { message: 'from js' }, 'JS exceptions propagate')
  assert.deepEqual(A.keysOf({ b: 1, a: 2 }), ['a', 'b'])
  assert.equal(A.add.name, 'add')
  assert.equal(A.add.length, 2)
})

test('errors and panics', () => {
  assert.throws(() => A.checkPositive(-2), { name: 'RangeError', code: 'E_NOT_POSITIVE', message: '-2 is not positive' })
  assert.equal(A.checkPositive(4), 4)
  assert.throws(() => A.plainError(), (e) => e.constructor === Error && e.message === 'plain failure' && e.code === undefined)
  assert.throws(() => A.panics(), { code: 'GO_PANIC', message: /framework boom/ })
  assert.equal(A.add(1, 1), 2, 'the addon keeps working after a panic')
  const before = A.procCalls()
  A.proc(5)
  assert.equal(A.procCalls(), before + 5)
  assert.equal(A.proc(1), undefined)
})

test('definitions with types that cannot be converted fail when the function is created', () => {
  const errs = A.badDefinitions()
  const expected = [
    /Func1: parameter 1: type chan int has no JS conversion/,
    /Func0: result: type func\(\) has no JS conversion/,
    /map keys must be strings or integers/,
    /Async1: parameter 1: type napi.Function refers to JS values/,
    /Async0: result: type napi.Object refers to JS values/,
    /Async1: parameter 1: type napi.Env cannot be used in a goroutine/,
    /type error cannot be decoded from JS/,
  ]
  assert.equal(errs.length, expected.length)
  expected.forEach((re, i) => assert.match(errs[i], re))
})

test('Async functions: Promises, errors and conversions', async () => {
  assert.equal(await A.addAsync(1, 2), 3)
  await assert.rejects(A.failAsync('E_ASYNC'), { name: 'TypeError', code: 'E_ASYNC', message: 'async failure' })
  assert.deepEqual(await A.userAsync('ann'), { id: 7, name: 'ann', age: 30, email: null, address: { city: 'Rome' } })
  const before = A.procCalls()
  assert.equal(await A.procAsync(3), undefined)
  assert.equal(A.procCalls(), before + 3)
  await assert.rejects(A.procAsync(-1), { message: 'negative' })
  // Arguments are converted before the goroutine starts: a bad argument throws synchronously.
  assert.throws(() => A.addAsync('x', 1), INVALID_TYPE)
  const c = await A.counterAsync(9)
  assert.ok(c instanceof A.Counter)
  assert.equal(c.value, 9)
  const many = 200
  const results = await Promise.all(Array.from({ length: many }, (_, i) => A.addAsync(i, i)))
  assert.deepEqual(results, Array.from({ length: many }, (_, i) => 2 * i))
})

test('constants', () => {
  assert.equal(A.version, '1.0.0')
  assert.deepEqual(A.limits, { max: 100, min: 0 })
})

test('classes: constructor, methods, accessors, statics and errors', () => {
  const c = new A.Counter(5)
  c.inc()
  assert.equal(c.add(2), 8)
  assert.equal(c.value, 8)
  c.value = 3
  assert.equal(c.value, 3)
  assert.equal(c.double, 6)
  assert.throws(() => { 'use strict'; c.double = 1 }, TypeError, 'read-only accessor')
  assert.equal(A.Counter.MAX, 100)
  assert.equal(A.Counter.from(4).value, 4)
  assert.equal(A.Counter.name, 'Counter')
  assert.deepEqual(Object.keys(c), [], 'methods and accessors live on the prototype')
  assert.deepEqual(Object.getOwnPropertyNames(A.Counter.prototype).sort(),
    ['add', 'clone', 'constructor', 'double', 'fail', 'inc', 'self', 'value'])
  assert.throws(() => new A.Counter(-1), { name: 'RangeError', code: 'E_NEGATIVE_START' })
  assert.throws(() => new A.Counter('x'), INVALID_TYPE)
  assert.throws(() => A.Counter(1), { name: 'TypeError', message: /cannot be invoked without 'new'/ })
  assert.throws(() => c.fail(), { message: 'counter failure' })
  assert.throws(() => c.add('x'), { ...INVALID_TYPE, message: 'argument 1: expected number, got string' })
  assert.throws(() => A.Counter.prototype.add.call({}, 1), TypeError, 'foreign this')
  const getter = Object.getOwnPropertyDescriptor(A.Counter.prototype, 'value').get
  assert.throws(() => getter.call({}), { name: 'TypeError', message: /this is not an instance of Counter/ })
  // Subclassing from JS keeps the Go value.
  class Sub extends A.Counter { twice () { return 2 * this.value } }
  const s = new Sub(21)
  assert.equal(s.twice(), 42)
  assert.equal(A.counterValue(s), 21)
})

test('classes: *T converts to the same instance, and from instances only', () => {
  const c = new A.Counter(1)
  assert.equal(c.self(), c, 'the same *T is the same object')
  assert.notEqual(c.clone(), c)
  assert.equal(c.clone().value, 1)
  assert.equal(A.counterValue(c), 1)
  assert.equal(A.counterValue(null), -1)
  assert.equal(A.counterValue(undefined), -1)
  assert.throws(() => A.counterValue({}), { ...INVALID_TYPE, message: 'argument 1: expected an instance of Counter, got an object' })
  const t = A.newToken('t1')
  assert.equal(t.id(), 't1')
  assert.equal(t.constructor.name, 'Token')
  assert.equal(A.Token, undefined, 'a registered class is not exported')
  assert.throws(() => new t.constructor(), { message: 'Class Token cannot be constructed from JS' })
  assert.throws(() => A.counterValue(t), INVALID_TYPE)
  assert.throws(() => A.Counter.prototype.add.call(t, 1), TypeError)
  assert.deepEqual(A.tokens(['a', 'b']).map((x) => x.id()), ['a', 'b'])
})

test('classes: Finalize runs once the last instance of a *T is collected', () => {
  const created = 50
  const r = nodeEval(`const A = require(${ADDON});
    const keep = new A.Counter(1);
    for (let i = 0; i < ${created}; i++) { const c = new A.Counter(i); c.self(); }
    const sleep = () => new Promise((resolve) => setTimeout(resolve, 10));
    (async () => {
      for (let i = 0; i < 50 && A.finalized() < ${created}; i++) { globalThis.gc(); await sleep(); }
      process.stdout.write(JSON.stringify({ finalized: A.finalized(), kept: keep.value }));
    })()`, { nodeArgs: ['--expose-gc'] })
  assert.equal(r.status, 0, r.stderr)
  const out = lastJSONLine(r)
  assert.equal(out.finalized, created, 'one Finalize per collected Counter, none for the live one')
  assert.equal(out.kept, 1)
})

test('classes: a *T returned while its collected instance awaits finalization gets a new instance, and Finalize waits for both', () => {
  const r = nodeEval(`const A = require(${ADDON});
    // Created in a function so that no interpreter register keeps the instance alive.
    (() => A.retain(new A.Counter(7)))();
    // The instance is unreachable: gc clears the weak reference at once, while the finalizer
    // runs later. Returning the same *T now creates a second wrapper.
    globalThis.gc();
    let again = A.retained();
    const value = again.value;
    const sleep = () => new Promise((resolve) => setTimeout(resolve, 10));
    (async () => {
      for (let i = 0; i < 5; i++) { globalThis.gc(); await sleep(); }
      const whileAlive = A.finalized();
      A.forget();
      again = null;
      for (let i = 0; i < 50 && A.finalized() === 0; i++) { globalThis.gc(); await sleep(); }
      process.stdout.write(JSON.stringify({ value, whileAlive, after: A.finalized() }));
    })()`, { nodeArgs: ['--expose-gc'] })
  assert.equal(r.status, 0, r.stderr)
  assert.deepEqual(lastJSONLine(r), { value: 7, whileAlive: 0, after: 1 })
})

test('exports and classes in worker threads', async () => {
  const w = new Worker(`
    const { parentPort } = require('node:worker_threads');
    const A = require(${ADDON});
    const c = new A.Counter(2);
    A.addAsync(c.add(3), 1).then((sum) => parentPort.postMessage({ sum, self: c.self() === c, version: A.version }));
  `, { eval: true })
  const exited = new Promise((resolve) => w.once('exit', resolve))
  const msg = await new Promise((resolve, reject) => { w.once('message', resolve); w.once('error', reject) })
  assert.deepEqual(msg, { sum: 6, self: true, version: '1.0.0' })
  await w.terminate()
  await exited
})

test('JSFunc: goroutines call JS and receive the converted result', async () => {
  const count = 3
  assert.deepEqual(await A.callFromGo((a) => `${a.note} ${a.n}`, count), ['from Go 0', 'from Go 1', 'from Go 2'])
  const delayMs = 5
  assert.deepEqual(await A.callFromGo(async (a) => { await new Promise((resolve) => setTimeout(resolve, delayMs)); return 'p' + a.n }, 2),
    ['p0', 'p1'], 'a returned Promise is awaited')
  await assert.rejects(A.callFromGo(() => { throw Object.assign(new RangeError('bad'), { code: 'E_JS' }) }, 1),
    { name: 'RangeError', code: 'E_JS', message: 'bad' })
  await assert.rejects(A.callFromGo(async () => { throw new TypeError('rejected') }, 1), { name: 'TypeError', message: 'rejected' })
  await assert.rejects(A.callFromGo(() => Promise.reject('plain'), 1), { message: 'plain' })
  // An error whose message getter throws: the message is replaced and no exception is left
  // pending (it would surface as uncaught).
  const r = nodeEval(`const A = require(${ADDON});
    const unreadable = () => { const e = new Error('x'); Object.defineProperty(e, 'message', { get () { throw new Error('getter') } }); throw e };
    A.callFromGo(unreadable, 1).catch((e) => process.stdout.write(JSON.stringify({ message: e.message })));`,
  { nodeArgs: ['--force-node-api-uncaught-exceptions-policy=true'] })
  assert.equal(r.status, 0, r.stderr)
  assert.deepEqual(lastJSONLine(r), { message: 'JavaScript error (message unreadable)' })
  assert.equal(r.stderr, '')
  await assert.rejects(A.callFromGo(() => 42, 1), { ...INVALID_TYPE, message: 'result: expected string, got number' })
  let args = -1
  assert.equal(await A.callNoArgs((...xs) => { args = xs.length }), undefined)
  assert.equal(args, 0, 'a struct{} argument calls without arguments')
  // Calls still queued when the function is aborted return ErrClosing.
  const blockMs = 150
  const p = A.abortWhileQueued((n) => n)
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, blockMs)
  assert.deepEqual(await p, Array(3).fill('napi: threadsafe function closing'))
  const bad = A.badJSFuncs(() => 0)
  assert.equal(bad.length, 3)
  assert.match(bad[0], /argument: type napi.Value refers to JS values/)
  assert.match(bad[1], /result: type napi.Object refers to JS values/)
  assert.match(bad[2], /argument: type chan int has no JS conversion/)
})

test('zero Go allocations per call for primitives, methods and accessors', () => {
  const calls = 20000
  const measure = (fn) => {
    fn()
    const before = A.goMallocs()
    for (let i = 0; i < calls; i++) fn()
    return (A.goMallocs() - before) / calls
  }
  // A ReadMemStats call between the two readings is counted in some Go versions.
  const tolerance = 0.01
  const c = new A.Counter(1)
  const buf = Buffer.alloc(4)
  const zero = {
    'noop()': () => A.noop(),
    'add(1, 2)': () => A.add(1, 2),
    'int8(1)': () => A.int8(1),
    'not(true)': () => A.not(true),
    'sumBytes(buf)': () => A.sumBytes(buf),
    'exclaim (Env, string → Value)': () => A.exclaim(''),
    'c.add(1)': () => c.add(1),
    'c.value': () => c.value,
    'c.value = 2': () => { c.value = 2 },
    'c.self()': () => c.self(),
    'counterValue(c)': () => A.counterValue(c),
  }
  for (const [name, fn] of Object.entries(zero)) {
    assert.ok(measure(fn) < tolerance, name)
  }
  // A string result small enough for the C buffer: only the concatenation in Go allocates.
  const oneAlloc = 1
  assert.ok(measure(() => A.greet('x')) <= oneAlloc + tolerance, 'greet')
})
