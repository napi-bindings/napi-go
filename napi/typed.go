package napi

// typed.go — Layer 2 functions on the typed trampoline of sys: primitive arguments arrive already
// converted by C (TypedArg) and primitive results are created by C (TypedOut), so a call such as
// add(a, b float64) float64 crosses cgo once instead of four times.

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// typedEntry is the Go side of a typed function: fn for functions and methods, get and set for
// an accessor (Node-API passes the same data to both: the getter is the call without arguments).
type typedEntry struct {
	fn, get, set *funcDef
}

// isMethod reports whether e runs on instances of a class (its receiver is unwrapped in C).
func (e *typedEntry) isMethod() bool {
	d := e.fn
	if d == nil {
		d = e.get
	}
	return d.kind == kindMethod
}

// typedEntries holds the typed functions reachable from JS, by the id stored in their TypedSig.
var typedEntries table[typedEntry]

func init() { sys.SetTypedCallbackHandler(dispatchTyped) }

func dispatchTyped(env sys.Env, info sys.CallbackInfo, id uintptr, this sys.Value, wrapped uintptr, args []sys.Value, slots []sys.TypedArg, out *sys.TypedOut) sys.Value {
	e := typedEntries.get(id)
	if e == nil {
		throwError(env, &Error{Code: sys.CodeInternal, Message: "napi-go: typed function not registered"})
		return nil
	}
	d := e.fn
	if d == nil {
		d = e.get
		if len(args) > 0 {
			d = e.set
		}
	}
	m := modeBorrow
	if d.kind == kindAsync {
		m = modeOwn
	}
	r, err := d.impl(call{env: env, info: info, this: this, args: args, d: d, m: m, slots: slots, out: out, wrapped: wrapped})
	if err != nil {
		throwError(env, err)
		return nil
	}
	return r
}

// typedKinds returns the kinds C converts for the JS parameters of d (d must be prepared).
func (d *funcDef) typedKinds() []sys.TypedKind {
	kinds := make([]sys.TypedKind, 0, d.jsLen)
	for _, p := range d.params {
		if p.inject != injectNone {
			continue
		}
		kinds = append(kinds, slotKind(p.c.prim))
	}
	return kinds
}

func slotKind(k primKind) sys.TypedKind {
	switch k {
	case primNone, primBytes:
		return sys.TypedValue
	case primBool:
		return sys.TypedBool
	case primString:
		return sys.TypedStr
	}
	return sys.TypedF64
}

// typedResources are the C signature and table entry of a typed function or property, released
// with the JS object that uses them.
type typedResources struct {
	id  uintptr
	sig sys.TypedSig
}

// newTyped registers e and allocates its C signature (with the parameter kinds of kindsOf).
func newTyped(e *typedEntry, kindsOf *funcDef) (typedResources, error) {
	id := typedEntries.add(e)
	var kinds []sys.TypedKind
	if kindsOf != nil {
		kinds = kindsOf.typedKinds()
	}
	var flags int32
	if e.isMethod() {
		flags = sys.TypedUnwrapThis
	}
	sig := sys.NewTypedSig(id, flags, kinds)
	if sig == 0 {
		typedEntries.remove(id)
		return typedResources{}, &Error{Code: sys.CodeInternal, Message: "napi-go: out of memory for a typed signature"}
	}
	return typedResources{id: id, sig: sig}, nil
}

func (r typedResources) release() {
	r.sig.Free()
	typedEntries.remove(r.id)
}

// newTypedFunction creates a JS function running d through the typed trampoline.
func (e Env) newTypedFunction(name string, d *funcDef) (Function, error) {
	res, err := newTyped(&typedEntry{fn: d}, d)
	if err != nil {
		return Function{}, err
	}
	var r sys.Value
	st := sys.CreateFunction(e.raw, unsafe.StringData(name), uint(len(name)), sys.TypedCallbackTrampoline, uintptr(res.sig), &r)
	v, err := e.value(r, st)
	if err != nil {
		res.release()
		return Function{}, err
	}
	f := Function{Object{v}}
	if err := e.releaseWith(f.Object, resources{typed: []typedResources{res}}); err != nil {
		return Function{}, err
	}
	return f, nil
}

// slotPrim decodes a primitive converted by C, if C converted it to the kind k needs. p does not
// escape.
func slotPrim(s *sys.TypedArg, k primKind, p unsafe.Pointer) (ok bool, err error) {
	switch s.Kind() {
	case sys.TypedF64:
		f := s.F64()
		switch k {
		case primFloat64:
			*(*float64)(p) = f
		case primFloat32:
			*(*float32)(p) = float32(f)
		case primBool, primString, primBytes:
			return false, nil
		default:
			return true, storeInt(f, k, p)
		}
	case sys.TypedBool:
		if k != primBool {
			return false, nil
		}
		*(*bool)(p) = s.Bool()
	case sys.TypedStr:
		if k != primString {
			return false, nil
		}
		*(*string)(p) = s.String()
	default:
		return false, nil
	}
	return true, nil
}

// outPrim sets a primitive result in out, for C to create. It reports false when C cannot (a
// string longer than the C buffer, []byte): the caller then creates the value in Go. p does not
// escape.
func outPrim(out *sys.TypedOut, k primKind, p unsafe.Pointer) bool {
	switch k {
	case primFloat64:
		out.SetF64(*(*float64)(p))
	case primFloat32:
		out.SetF64(float64(*(*float32)(p)))
	case primBool:
		out.SetBool(*(*bool)(p))
	case primString:
		return out.SetString(*(*string)(p))
	case primInt8:
		out.SetInt32(int32(*(*int8)(p)))
	case primInt16:
		out.SetInt32(int32(*(*int16)(p)))
	case primInt32:
		out.SetInt32(*(*int32)(p))
	case primUint8:
		out.SetUint32(uint32(*(*uint8)(p)))
	case primUint16:
		out.SetUint32(uint32(*(*uint16)(p)))
	case primUint32:
		out.SetUint32(*(*uint32)(p))
	case primInt:
		out.SetF64(float64(*(*int)(p)))
	case primInt64:
		out.SetF64(float64(*(*int64)(p)))
	case primUint:
		out.SetF64(float64(*(*uint)(p)))
	case primUint64:
		out.SetF64(float64(*(*uint64)(p)))
	case primUintptr:
		out.SetF64(float64(*(*uintptr)(p)))
	default:
		return false
	}
	return true
}
