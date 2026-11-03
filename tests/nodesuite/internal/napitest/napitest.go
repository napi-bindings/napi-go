// Package napitest is the Go equivalent of test/js-native-api/common.h and common-inl.h from the
// Node.js test suite: functions match the behavior of the C macros and produce the same messages,
// allowing the original JS test scripts to run unmodified against the Go-ported addons.
//
// Mappings:
//
//	NODE_API_CALL(env, call)              → if !napitest.Call(env, call) { return nil }
//	NODE_API_CALL_RETURN_VOID(env, call)  → if !napitest.Call(env, call) { return }
//	NODE_API_ASSERT(env, cond, msg)       → if !napitest.Assert(env, cond, "<cond in C>", msg) { return nil }
//	GET_AND_THROW_LAST_ERROR(env)         → napitest.GetAndThrowLastError(env)
//	DECLARE_NODE_API_PROPERTY(name, fn)   → napitest.DeclareProperty(name, fn)
//	DECLARE_NODE_API_GETTER(name, fn)     → napitest.DeclareGetter(name, fn)
//	DECLARE_NODE_API_PROPERTY_VALUE(n, v) → napitest.DeclarePropertyValue(n, v)
//	add_returned_status / add_last_status → napitest.AddReturnedStatus / napitest.AddLastStatus
//	NAPI_MODULE / NAPI_MODULE_INIT        → napitest.ModuleInit(init) called from init()
//
// The condition text passed to Assert must be copied verbatim from the C source, because the macro
// embeds it into the message via preprocessor stringification and the JS scripts assert on it.
//
// JS functions created from Go go through sys.CallbackTrampoline: the napi data field carries the
// index of the registered Go function (Register), not the C test's data.
package napitest

/*
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Func is the Go equivalent of napi_callback.
type Func func(env sys.Env, info sys.CallbackInfo) sys.Value

// Literal messages from common.h / common-inl.h.
const (
	emptyErrorMessage   = "empty error message"
	lastStatusOK        = "napi_ok"
	exceptionKeySuffix  = "Exception"
	invalidStatusPrefix = "Invalid status ["
	invalidStatusSuffix = "]"
	unavailableMessage  = "napitest: Node-API function not exported by this Node.js"
)

// entry is a registered function; argc is the argument count of the JS call and is used only by
// accessors, where getter and setter share the same data.
type entry func(env sys.Env, info sys.CallbackInfo, argc int) sys.Value

var (
	registryMu sync.Mutex
	// registry is copy-on-write: reads from CallbackHandler take no lock, even when a worker
	// registers functions during its own Init. Index 0 is reserved: a NULL data never matches a
	// function.
	registry atomic.Pointer[[]entry]

	cstrings sync.Map // string → *byte in C memory, never freed
)

func init() {
	reserved := []entry{nil}
	registry.Store(&reserved)
	sys.SetCallbackHandler(dispatchCallback)
	sys.SetAsyncExecuteHandler(dispatchAsyncExecute)
	sys.SetAsyncCompleteHandler(dispatchAsyncComplete)
	sys.SetFinalizeHandler(dispatchFinalize)
	sys.SetCallJSHandler(dispatchCallJS)
	sys.SetCleanupHookHandler(dispatchCleanupHook)
	sys.SetAsyncCleanupHookHandler(dispatchAsyncCleanupHook)
}

func dispatchCallback(env sys.Env, info sys.CallbackInfo, data uintptr, _ sys.Value, args []sys.Value) sys.Value {
	fns := *registry.Load()
	if data >= uintptr(len(fns)) || fns[data] == nil {
		throwMessage(env, "napitest: unknown callback index "+strconv.FormatUint(uint64(data), 10))
		return nil
	}
	return fns[data](env, info, len(args))
}

// ModuleInit registers the module initialization function (Init in NAPI_MODULE). It must be
// called from init() in the addon's main package.
func ModuleInit(fn func(env sys.Env, exports sys.Value) sys.Value) {
	sys.SetModuleInitHandler(fn)
}

func register(e entry) uintptr {
	registryMu.Lock()
	defer registryMu.Unlock()
	old := *registry.Load()
	next := make([]entry, len(old)+1)
	copy(next, old)
	next[len(old)] = e
	registry.Store(&next)
	return uintptr(len(old))
}

// Register makes fn callable from JS and returns the value to pass as data along with
// sys.CallbackTrampoline (napi_create_function, napi_define_class, property descriptors). Each
// call appends an entry: functions created repeatedly (factories) must be registered once at
// package level.
func Register(fn Func) uintptr {
	if fn == nil {
		panic("napitest: Register(nil)")
	}
	return register(func(env sys.Env, info sys.CallbackInfo, _ int) sys.Value { return fn(env, info) })
}

// RegisterAccessor registers a getter/setter pair that Node-API calls with the same data: a
// zero-argument call invokes the getter, and a one-argument call invokes the setter. setter may be
// nil.
func RegisterAccessor(getter, setter Func) uintptr {
	if getter == nil {
		panic("napitest: RegisterAccessor with nil getter")
	}
	return register(func(env sys.Env, info sys.CallbackInfo, argc int) sys.Value {
		if argc == 0 || setter == nil {
			return getter(env, info)
		}
		return setter(env, info)
	})
}

// CStr returns a NUL-terminated copy of s in C memory. Strings are shared and never freed: use
// them only for names and constants, so they can also be stored inside structs passed to C (e.g.
// sys.PropertyDescriptor) without violating cgo pointer rules.
func CStr(s string) *byte {
	if p, ok := cstrings.Load(s); ok {
		return p.(*byte)
	}
	p := (*byte)(unsafe.Pointer(C.CString(s)))
	if prev, loaded := cstrings.LoadOrStore(s, p); loaded {
		C.free(unsafe.Pointer(p))
		return prev.(*byte)
	}
	return p
}

// GoString copies a NUL-terminated C string; nil becomes "".
func GoString(p *byte) string {
	if p == nil {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(p)))
}

// withCString calls fn with a temporary copy of s in C memory.
func withCString(s string, fn func(p *byte)) {
	p := C.CString(s)
	defer C.free(unsafe.Pointer(p))
	fn((*byte)(unsafe.Pointer(p)))
}

// ThrowTypeError corresponds to napi_throw_type_error(env, NULL, msg) with the same stderr
// fallback as throw.
func ThrowTypeError(env sys.Env, msg string) {
	withCString(msg, func(p *byte) {
		if st := sys.ThrowTypeError(env, nil, p); st != sys.StatusOK {
			fmt.Fprintf(os.Stderr, "napitest: napi_throw_type_error(%q) failed: %s\n", msg, st)
		}
	})
}

func throwMessage(env sys.Env, msg string) {
	withCString(msg, func(p *byte) { throw(env, p) })
}

// throw throws a JS Error with msg; if napi_throw_error fails, the message is at least written to
// stderr so the test fails with a readable cause rather than silently.
func throw(env sys.Env, msg *byte) {
	if st := sys.ThrowError(env, nil, msg); st != sys.StatusOK {
		fmt.Fprintf(os.Stderr, "napitest: napi_throw_error(%q) failed: %s\n", GoString(msg), st)
	}
}

// GetAndThrowLastError corresponds to GET_AND_THROW_LAST_ERROR: throws a JS Error with the last
// Node-API error message unless an exception is already pending.
func GetAndThrowLastError(env sys.Env) {
	var info *sys.ExtendedErrorInfo
	var msg *byte
	// If retrieving the error info fails, info stays nil and "empty error message" is used,
	// matching the C macro.
	if sys.GetLastErrorInfo(env, &info) == sys.StatusOK && info != nil {
		msg = info.ErrorMessage
	}
	var pending bool
	// If checking fails, throw the error anyway: better an extra exception than losing one.
	if sys.IsExceptionPending(env, &pending) != sys.StatusOK {
		pending = false
	}
	// Do not overwrite an already pending exception.
	if pending {
		return
	}
	if msg == nil {
		msg = CStr(emptyErrorMessage)
	}
	throw(env, msg)
}

// Call corresponds to NODE_API_CALL_BASE: if status is not StatusOK, throws the last error and
// returns false, and the caller must return as the macro would.
func Call(env sys.Env, status sys.Status) bool {
	if status == sys.StatusUnavailable {
		// Not a Node-API status: the function is missing from the running Node binary.
		throwMessage(env, unavailableMessage)
		return false
	}
	if status != sys.StatusOK {
		GetAndThrowLastError(env)
		return false
	}
	return true
}

// Assert corresponds to NODE_API_ASSERT_BASE. expr is the C condition text as produced by macro
// stringification.
func Assert(env sys.Env, cond bool, expr, message string) bool {
	if !cond {
		throwMessage(env, "assertion ("+expr+") failed: "+message)
		return false
	}
	return true
}

// AssertStatus corresponds to NODE_API_ASSERT_STATUS: if cond is false, throws the Assert error
// and returns StatusGenericFailure, otherwise StatusOK.
func AssertStatus(env sys.Env, cond bool, expr, message string) sys.Status {
	if !Assert(env, cond, expr, message) {
		return sys.StatusGenericFailure
	}
	return sys.StatusOK
}

// DeclareProperty corresponds to DECLARE_NODE_API_PROPERTY.
func DeclareProperty(name string, fn Func) sys.PropertyDescriptor {
	return sys.PropertyDescriptor{
		Utf8Name:   CStr(name),
		Method:     sys.CallbackTrampoline,
		Attributes: sys.PropertyDefault,
		Data:       Register(fn),
	}
}

// DeclareGetter corresponds to DECLARE_NODE_API_GETTER.
func DeclareGetter(name string, fn Func) sys.PropertyDescriptor {
	return sys.PropertyDescriptor{
		Utf8Name:   CStr(name),
		Getter:     sys.CallbackTrampoline,
		Attributes: sys.PropertyDefault,
		Data:       Register(fn),
	}
}

// DeclarePropertyValue corresponds to DECLARE_NODE_API_PROPERTY_VALUE.
func DeclarePropertyValue(name string, value sys.Value) sys.PropertyDescriptor {
	return sys.PropertyDescriptor{
		Utf8Name:   CStr(name),
		Value:      value,
		Attributes: sys.PropertyDefault,
	}
}

// Method corresponds to { name, NULL, fn, NULL, NULL, NULL, attributes, NULL }.
func Method(name string, fn Func, attributes sys.PropertyAttributes) sys.PropertyDescriptor {
	d := DeclareProperty(name, fn)
	d.Attributes = attributes
	return d
}

// Value corresponds to { name, NULL, NULL, NULL, NULL, value, attributes, NULL }.
func Value(name string, value sys.Value, attributes sys.PropertyAttributes) sys.PropertyDescriptor {
	d := DeclarePropertyValue(name, value)
	d.Attributes = attributes
	return d
}

// Accessor corresponds to { name, NULL, NULL, getter, setter, NULL, attributes, NULL }; setter
// may be nil. Getter and setter share data, as in Node-API (see RegisterAccessor).
func Accessor(name string, getter, setter Func, attributes sys.PropertyAttributes) sys.PropertyDescriptor {
	d := sys.PropertyDescriptor{
		Utf8Name:   CStr(name),
		Getter:     sys.CallbackTrampoline,
		Attributes: attributes,
		Data:       RegisterAccessor(getter, setter),
	}
	if setter != nil {
		d.Setter = sys.CallbackTrampoline
	}
	return d
}

// DefineProperties calls napi_define_properties over all of props (sizeof(props)/sizeof(*props)
// in C). When props is empty it passes a NULL pointer.
func DefineProperties(env sys.Env, object sys.Value, props []sys.PropertyDescriptor) sys.Status {
	if len(props) == 0 {
		return sys.DefineProperties(env, object, 0, nil)
	}
	return sys.DefineProperties(env, object, uint(len(props)), &props[0])
}

// AddReturnedStatus corresponds to add_returned_status in common-inl.h: sets object[key] to
// expectedMessage if actual == expected, or to "Invalid status [<actual>]" otherwise.
func AddReturnedStatus(env sys.Env, key string, object sys.Value, expectedMessage string,
	expected, actual sys.Status) {
	msg := expectedMessage
	if actual != expected {
		msg = invalidStatusPrefix + strconv.Itoa(int(actual)) + invalidStatusSuffix
	}
	var prop sys.Value
	ok := true
	withCString(msg, func(p *byte) {
		ok = Call(env, sys.CreateStringUTF8(env, p, sys.AutoLength, &prop))
	})
	if !ok {
		return
	}
	withCString(key, func(p *byte) { Call(env, sys.SetNamedProperty(env, object, p, prop)) })
}

// AddLastStatus corresponds to add_last_status in common-inl.h: sets returnValue[key] to the last
// error message ("napi_ok" if none) and, if an exception is pending, clears it and stores it in
// returnValue[key+"Exception"].
func AddLastStatus(env sys.Env, key string, returnValue sys.Value) {
	var last *sys.ExtendedErrorInfo
	if !Call(env, sys.GetLastErrorInfo(env, &last)) {
		return
	}
	// The contents of last change on the next Node-API call: copy it immediately.
	message := lastStatusOK
	if last != nil && last.ErrorMessage != nil {
		message = GoString(last.ErrorMessage)
	}
	var pending bool
	if !Call(env, sys.IsExceptionPending(env, &pending)) {
		return
	}
	if pending {
		var exception sys.Value
		if !Call(env, sys.GetAndClearLastException(env, &exception)) {
			return
		}
		ok := true
		withCString(key+exceptionKeySuffix, func(p *byte) {
			ok = Call(env, sys.SetNamedProperty(env, returnValue, p, exception))
		})
		if !ok {
			return
		}
	}
	var prop sys.Value
	ok := true
	withCString(message, func(p *byte) {
		ok = Call(env, sys.CreateStringUTF8(env, p, sys.AutoLength, &prop))
	})
	if !ok {
		return
	}
	withCString(key, func(p *byte) { Call(env, sys.SetNamedProperty(env, returnValue, p, prop)) })
}
