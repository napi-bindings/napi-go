// Layer 0 test addon: uses only the sys package, just as Layer 1 does.
//
// Each exported function receives its index in `exports` as its callback data; sys's
// CallbackHandler uses it for dispatch.
package main

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

func main() {}

type jsFunc func(env sys.Env, this sys.Value, args []sys.Value) sys.Value

type export struct {
	name string
	fn   jsFunc
}

// Finalize hints: they tell apart the finalizers sharing the FinalizeHandler.
const (
	// hintExternal: Externals created by makeExternals.
	hintExternal = 1
	// hintTSFN: threadsafe functions created by tsfnCollect (data is the tsfnLife id).
	hintTSFN = 2
)

// addonName is overridden with -ldflags=-X to build distinct copies of the same addon (two Go
// runtimes in the same process).
var addonName = "sysbasic"

var (
	exports     []export
	helloIndex  uintptr
	finalized   atomic.Int64
	initedEnvs  atomic.Int64
	cleanedEnvs atomic.Int64
	// tsfnErrors counts Call/ReleaseThreadsafeFunction failures inside goroutines, where a JS
	// exception cannot be thrown: tests verify that it stays zero.
	tsfnErrors atomic.Int64
)

func init() {
	exports = []export{
		{"declaredVersion", func(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
			return newNum(env, float64(sys.DeclaredVersion))
		}},
		{"moduleAPIVersion", func(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
			return newNum(env, float64(sys.ModuleAPIVersion()))
		}},
		{"name", func(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value { return newStr(env, addonName) }},
		{"available", available},
		{"add", add},
		{"echo", echo},
		{"countArgs", func(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
			return newNum(env, float64(len(args)))
		}},
		{"sumArgs", sumArgs},
		{"thisValue", func(_ sys.Env, this sys.Value, _ []sys.Value) sys.Value { return this }},
		{"panicNow", func(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
			msg, _ := argString(env, args, 0)
			panic("boom: " + msg)
		}},
		{"throwCoded", throwCoded},
		{"lastError", lastError},
		{"tagObject", tagObject},
		{"checkTag", checkTag},
		{"hello", func(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value { return newStr(env, "hello") }},
		{"defineProps", defineProps},
		{"makeExternals", makeExternals},
		{"finalizedCount", func(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
			return newNum(env, float64(finalized.Load()))
		}},
		{"nodeVersion", nodeVersion},
		{"moduleFileName", moduleFileName},
		{"symbolFor", symbolFor},
		{"propertyKey", propertyKey},
		{"callMe", callMe},
		{"tsfnCollect", tsfnCollect},
		{"asyncSum", asyncSum},
		{"allocsPerCall", allocsPerCall},
		// process.go: Go runtime load and signal handler hooks (tests/e2e/process.test.mjs).
		{"burnCPU", burnCPU},
		{"burning", burning},
		{"goGC", goGC},
		{"dropResetHand", dropResetHand},
		{"repairSignalHandlers", repairSignalHandlers},
		{"envCounts", func(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
			obj := newObject(env)
			setNum(env, obj, "inited", float64(initedEnvs.Load()))
			setNum(env, obj, "cleaned", float64(cleanedEnvs.Load()))
			setNum(env, obj, "tsfnErrors", float64(tsfnErrors.Load()))
			return obj
		}},
	}
	for i, e := range exports {
		if e.name == "hello" {
			helloIndex = uintptr(i)
		}
	}

	sys.SetModuleInitHandler(initModule)
	sys.SetCallbackHandler(func(env sys.Env, _ sys.CallbackInfo, data uintptr, this sys.Value, args []sys.Value) sys.Value {
		return exports[data].fn(env, this, args)
	})
	sys.SetFinalizeHandler(func(_ sys.Env, data, hint uintptr) {
		switch hint {
		case hintExternal:
			finalized.Add(1)
		case hintTSFN:
			finalizeTSFN(data)
		}
	})
	sys.SetCallJSHandler(onCallJS)
	sys.SetAsyncExecuteHandler(onAsyncExecute)
	sys.SetAsyncCompleteHandler(onAsyncComplete)
	sys.SetCleanupHookHandler(func(uintptr) { cleanedEnvs.Add(1) })
}

func initModule(env sys.Env, obj sys.Value) sys.Value {
	for i, e := range exports {
		var fn sys.Value
		must(sys.CreateFunction(env, cstr(e.name), sys.AutoLength, sys.CallbackTrampoline, uintptr(i), &fn))
		must(sys.SetNamedProperty(env, obj, cstr(e.name), fn))
	}
	must(sys.AddEnvCleanupHook(env, sys.CleanupHookTrampoline, 0))
	initedEnvs.Add(1)
	return obj
}

// --- helpers ----------------------------------------------------------------------------------

// must turns an unexpected status into a panic: sys converts it into a GO_PANIC JS Error.
func must(st sys.Status) {
	if st != sys.StatusOK {
		panic(fmt.Sprintf("status %s", st))
	}
}

var nul = [1]byte{}

// cstr returns s NUL-terminated.
func cstr(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// strPtr returns a pointer to the bytes of s, valid even for an empty string.
func strPtr(s string) *byte {
	if len(s) == 0 {
		return &nul[0]
	}
	return unsafe.StringData(s)
}

func goStringC(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

func throwType(env sys.Env, msg string) sys.Value {
	// If an exception is already pending, keep it; any other failure is an addon bug.
	if st := sys.ThrowTypeError(env, nil, cstr(msg)); st != sys.StatusOK && st != sys.StatusPendingException {
		panic(fmt.Sprintf("napi_throw_type_error: %s", st))
	}
	return nil
}

func newNum(env sys.Env, d float64) sys.Value {
	var v sys.Value
	must(sys.CreateDouble(env, d, &v))
	return v
}

func newStr(env sys.Env, s string) sys.Value {
	var v sys.Value
	must(sys.CreateStringUTF8(env, strPtr(s), uint(len(s)), &v))
	return v
}

func newBool(env sys.Env, b bool) sys.Value {
	var v sys.Value
	must(sys.GetBoolean(env, b, &v))
	return v
}

func newObject(env sys.Env) sys.Value {
	var v sys.Value
	must(sys.CreateObject(env, &v))
	return v
}

func undefined(env sys.Env) sys.Value {
	var v sys.Value
	must(sys.GetUndefined(env, &v))
	return v
}

func setNum(env sys.Env, obj sys.Value, key string, d float64) {
	must(sys.SetNamedProperty(env, obj, cstr(key), newNum(env, d)))
}

func setStr(env sys.Env, obj sys.Value, key, s string) {
	must(sys.SetNamedProperty(env, obj, cstr(key), newStr(env, s)))
}

func argNum(env sys.Env, args []sys.Value, i int) (float64, bool) {
	if i >= len(args) {
		return 0, false
	}
	var d float64
	return d, sys.GetValueDouble(env, args[i], &d) == sys.StatusOK
}

// stackStringMax: shorter strings are read into a stack buffer (noescape + nocallback: zero
// allocations for the buffer).
const stackStringMax = 64

func argString(env sys.Env, args []sys.Value, i int) (string, bool) {
	if i >= len(args) {
		return "", false
	}
	var n uint
	if sys.GetValueStringUTF8(env, args[i], nil, 0, &n) != sys.StatusOK {
		return "", false
	}
	var written uint
	if n < stackStringMax {
		var buf [stackStringMax]byte
		must(sys.GetValueStringUTF8(env, args[i], &buf[0], uint(len(buf)), &written))
		return string(buf[:written]), true
	}
	buf := make([]byte, n+1)
	must(sys.GetValueStringUTF8(env, args[i], &buf[0], n+1, &written))
	return string(buf[:written]), true
}

// --- exported functions -----------------------------------------------------------------------

func available(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	name, ok := argString(env, args, 0)
	if !ok {
		return throwType(env, "A string was expected")
	}
	f, ok := sys.FnByName(name)
	if !ok {
		return throwType(env, "unknown Node-API function: "+name)
	}
	return newBool(env, sys.Available(f))
}

func add(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	a, okA := argNum(env, args, 0)
	b, okB := argNum(env, args, 1)
	if !okA || !okB {
		return throwType(env, "A number was expected")
	}
	return newNum(env, a+b)
}

func echo(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	s, ok := argString(env, args, 0)
	if !ok {
		return throwType(env, "A string was expected")
	}
	return newStr(env, s)
}

func sumArgs(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	total := 0.0
	for i := range args {
		d, ok := argNum(env, args, i)
		if !ok {
			return throwType(env, "A number was expected")
		}
		total += d
	}
	return newNum(env, total)
}

func throwCoded(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	code, okC := argString(env, args, 0)
	msg, okM := argString(env, args, 1)
	if !okC || !okM {
		return throwType(env, "A string was expected")
	}
	must(sys.ThrowError(env, cstr(code), cstr(msg)))
	return nil
}

// lastError triggers a Node-API error and reads its extended error info.
func lastError(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
	var d float64
	st := sys.GetValueDouble(env, newStr(env, "not a number"), &d)
	var info *sys.ExtendedErrorInfo
	must(sys.GetLastErrorInfo(env, &info))
	// info points into the env's state: valid only until the next Node-API call.
	infoStatus, message := info.ErrorCode, goStringC(info.ErrorMessage)
	obj := newObject(env)
	setStr(env, obj, "status", st.String())
	setStr(env, obj, "infoStatus", infoStatus.String())
	setStr(env, obj, "message", message)
	return obj
}

func tagArgs(env sys.Env, args []sys.Value) (sys.TypeTag, bool) {
	lo, okL := argNum(env, args, 1)
	hi, okH := argNum(env, args, 2)
	return sys.TypeTag{Lower: uint64(lo), Upper: uint64(hi)}, okL && okH && len(args) > 0
}

func tagObject(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	tag, ok := tagArgs(env, args)
	if !ok {
		return throwType(env, "tagObject(obj, lower, upper)")
	}
	return newStr(env, sys.TypeTagObject(env, args[0], &tag).String())
}

func checkTag(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	tag, ok := tagArgs(env, args)
	if !ok {
		return throwType(env, "checkTag(obj, lower, upper)")
	}
	var match bool
	must(sys.CheckObjectTypeTag(env, args[0], &tag, &match))
	return newBool(env, match)
}

// defineProps uses PropertyDescriptor in Go memory: the name pointers must be pinned with
// runtime.Pinner to satisfy cgo rules (verified by GOEXPERIMENT=cgocheck2).
func defineProps(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	if len(args) < 1 {
		return throwType(env, "defineProps(obj)")
	}
	var pinner runtime.Pinner
	defer pinner.Unpin()
	hello, answer := cstr("hello"), cstr("answer")
	pinner.Pin(hello)
	pinner.Pin(answer)
	props := [...]sys.PropertyDescriptor{
		{Utf8Name: hello, Method: sys.CallbackTrampoline, Data: helloIndex, Attributes: sys.PropertyDefault},
		{Utf8Name: answer, Value: newNum(env, 42), Attributes: sys.PropertyEnumerable},
	}
	must(sys.DefineProperties(env, args[0], uint(len(props)), &props[0]))
	return args[0]
}

func makeExternals(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	n, ok := argNum(env, args, 0)
	if !ok || n < 0 {
		return throwType(env, "makeExternals(n)")
	}
	for i := 0; i < int(n); i++ {
		var v sys.Value
		must(sys.CreateExternal(env, uintptr(i+1), sys.FinalizeTrampoline, hintExternal, &v))
	}
	return undefined(env)
}

func nodeVersion(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
	var v *sys.NodeVersion
	must(sys.GetNodeVersion(env, &v))
	obj := newObject(env)
	setNum(env, obj, "major", float64(v.Major))
	setNum(env, obj, "minor", float64(v.Minor))
	setNum(env, obj, "patch", float64(v.Patch))
	setStr(env, obj, "release", goStringC(v.Release))
	return obj
}

func moduleFileName(env sys.Env, _ sys.Value, _ []sys.Value) sys.Value {
	if !sys.Available(sys.FnGetModuleFileName) {
		return undefined(env)
	}
	var p *byte
	must(sys.GetModuleFileName(env, &p))
	return newStr(env, goStringC(p))
}

func symbolFor(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	s, ok := argString(env, args, 0)
	if !ok {
		return throwType(env, "A string was expected")
	}
	var v sys.Value
	if st := sys.SymbolFor(env, strPtr(s), uint(len(s)), &v); st != sys.StatusOK {
		return throwType(env, "node_api_symbol_for: "+st.String())
	}
	return v
}

func propertyKey(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	s, ok := argString(env, args, 0)
	if !ok {
		return throwType(env, "A string was expected")
	}
	var v sys.Value
	if st := sys.CreatePropertyKeyUTF8(env, strPtr(s), uint(len(s)), &v); st != sys.StatusOK {
		return throwType(env, "node_api_create_property_key_utf8: "+st.String())
	}
	return v
}

// callMe(fn, ...args) calls fn from Go: tests JS → Go → JS (→ Go) reentrancy and JS exception
// propagation across Go frames.
func callMe(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	if len(args) < 1 {
		return throwType(env, "callMe(fn, ...args)")
	}
	var global, result sys.Value
	must(sys.GetGlobal(env, &global))
	rest := args[1:]
	var argv *sys.Value
	if len(rest) > 0 {
		argv = &rest[0]
	}
	if st := sys.CallFunction(env, global, args[0], uint(len(rest)), argv, &result); st != sys.StatusOK {
		return nil // pending JS exception: Node propagates it to the caller
	}
	return result
}

// tsfnLife guards a threadsafe function against its destruction. When the env is torn down
// (worker terminated, process exiting) Node finalizes and frees the TSFN even if threads still
// hold it: calling it afterwards is a use-after-free. The finalizer marks it dead under the
// write lock; the goroutine holds the read lock across each call and stops once it is dead.
type tsfnLife struct {
	mu   sync.RWMutex
	dead bool
}

var (
	tsfnLivesMu sync.Mutex
	tsfnLives   = map[uintptr]*tsfnLife{}
	tsfnSeq     uintptr
)

// finalizeTSFN runs on the JS thread when Node destroys the threadsafe function: it waits for an
// in-flight call to return and prevents any further one.
func finalizeTSFN(id uintptr) {
	tsfnLivesMu.Lock()
	life := tsfnLives[id]
	delete(tsfnLives, id)
	tsfnLivesMu.Unlock()
	if life == nil {
		return
	}
	life.mu.Lock()
	life.dead = true
	life.mu.Unlock()
}

// whileAlive runs f with the TSFN alive and reports false once the TSFN has been destroyed.
func (l *tsfnLife) whileAlive(f func() sys.Status) (sys.Status, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if l.dead {
		return sys.StatusClosing, false
	}
	return f(), true
}

// tsfnCollect(n, cb): a goroutine calls cb(0..n-1) through a threadsafe function.
func tsfnCollect(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	n, ok := argNum(env, args, 0)
	if !ok || n < 0 || len(args) < 2 {
		return throwType(env, "tsfnCollect(n, cb)")
	}
	life := &tsfnLife{}
	tsfnLivesMu.Lock()
	tsfnSeq++
	id := tsfnSeq
	tsfnLives[id] = life
	tsfnLivesMu.Unlock()
	var tsfn sys.ThreadsafeFunction
	if st := sys.CreateThreadsafeFunction(env, args[1], nil, newStr(env, "tsfnCollect"), 0, 1,
		id, sys.FinalizeTrampoline, hintTSFN, sys.CallJSTrampoline, &tsfn); st != sys.StatusOK {
		finalizeTSFN(id)
		must(st)
	}
	count := int(n)
	go func() {
		for i := 0; i < count; i++ {
			st, alive := life.whileAlive(func() sys.Status { return sys.CallThreadsafeFunction(tsfn, uintptr(i), sys.TSFNBlocking) })
			if !alive {
				return // destroyed with the env: it must not be released either
			}
			if st != sys.StatusOK {
				tsfnErrors.Add(1) // e.g. StatusClosing: TSFN closing because the env was torn down
				break
			}
		}
		if st, alive := life.whileAlive(func() sys.Status { return sys.ReleaseThreadsafeFunction(tsfn, sys.TSFNRelease) }); alive && st != sys.StatusOK {
			tsfnErrors.Add(1)
		}
	}()
	return undefined(env)
}

func onCallJS(env sys.Env, cb sys.Value, _, data uintptr) {
	if env == nil {
		return // TSFN destroyed with items still queued
	}
	arg := newNum(env, float64(data))
	// An exception thrown by cb stays pending and Node reports it as uncaught: that is the
	// intended behavior. Any other status is an addon error.
	if st := sys.CallFunction(env, undefined(env), cb, 1, &arg, nil); st != sys.StatusOK && st != sys.StatusPendingException {
		panic(fmt.Sprintf("napi_call_function: %s", st))
	}
}

// --- async work -------------------------------------------------------------------------------

type asyncJob struct {
	n        int
	sum      float64
	deferred sys.Deferred
	work     sys.AsyncWork
}

var (
	jobsMu sync.Mutex
	jobs   = map[uintptr]*asyncJob{}
	jobSeq uintptr
)

// asyncSum(n): sums 0..n-1 on a libuv thread-pool worker and resolves a Promise.
func asyncSum(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	n, ok := argNum(env, args, 0)
	if !ok || n < 0 {
		return throwType(env, "asyncSum(n)")
	}
	job := &asyncJob{n: int(n)}
	var promise sys.Value
	must(sys.CreatePromise(env, &job.deferred, &promise))
	jobsMu.Lock()
	jobSeq++
	id := jobSeq
	jobs[id] = job
	jobsMu.Unlock()
	must(sys.CreateAsyncWork(env, nil, newStr(env, "asyncSum"), sys.AsyncExecuteTrampoline,
		sys.AsyncCompleteTrampoline, id, &job.work))
	must(sys.QueueAsyncWork(env, job.work))
	return promise
}

func lookupJob(id uintptr) *asyncJob {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	return jobs[id]
}

func onAsyncExecute(_ sys.Env, id uintptr) {
	job := lookupJob(id)
	for i := 0; i < job.n; i++ {
		job.sum += float64(i)
	}
}

func onAsyncComplete(env sys.Env, status sys.Status, id uintptr) {
	job := lookupJob(id)
	jobsMu.Lock()
	delete(jobs, id)
	jobsMu.Unlock()
	must(sys.DeleteAsyncWork(env, job.work))
	if status != sys.StatusOK {
		mustUnlessShuttingDown(sys.RejectDeferred(env, job.deferred, newStr(env, "async work: "+status.String())))
		return
	}
	mustUnlessShuttingDown(sys.ResolveDeferred(env, job.deferred, newNum(env, job.sum)))
}

// mustUnlessShuttingDown is must for calls that run JavaScript (settling a Promise): when the
// worker or the process is shutting down they fail with napi_pending_exception or
// napi_cannot_run_js, and the Promise can no longer be observed by anyone.
func mustUnlessShuttingDown(st sys.Status) {
	if st != sys.StatusPendingException && st != sys.StatusCannotRunJS {
		must(st)
	}
}

// --- allocations ------------------------------------------------------------------------------

const allocIterations = 10_000

// propertyK is the NUL-terminated name "k", kept global to avoid allocating inside the measured loop.
var propertyK = [...]byte{'k', 0}

// allocsPerCall(obj, str) measures Go allocations per iteration for a group of calls:
// creating and reading numbers, typeof, reading a property (a function that can execute JS and
// therefore lacks nocallback), and reading a string into a stack buffer.
func allocsPerCall(env sys.Env, _ sys.Value, args []sys.Value) sys.Value {
	if len(args) < 2 {
		return throwType(env, "allocsPerCall(obj, str)")
	}
	obj, str := args[0], args[1]
	var scope sys.HandleScope
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	before := ms.Mallocs
	for i := 0; i < allocIterations; i++ {
		must(sys.OpenHandleScope(env, &scope))
		var v, p sys.Value
		var d float64
		var t sys.ValueType
		var buf [16]byte
		var written uint
		must(sys.CreateDouble(env, 1.5, &v))
		must(sys.GetValueDouble(env, v, &d))
		must(sys.Typeof(env, v, &t))
		must(sys.GetNamedProperty(env, obj, &propertyK[0], &p))
		must(sys.GetValueStringUTF8(env, str, &buf[0], uint(len(buf)), &written))
		must(sys.CloseHandleScope(env, scope))
	}
	runtime.ReadMemStats(&ms)
	return newNum(env, float64(ms.Mallocs-before)/allocIterations)
}
