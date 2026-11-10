package napi

// export.go — Layer 2: typed functions (FuncDef) and the export registry.
//
// The wrappers in zz_wrappers.go (Func2, Async1, Method0, ...) turn a Go function into a funcDef:
// its parameter and result types plus an impl that decodes the arguments with arg, calls the Go
// function and encodes the result with result. Codecs are resolved when the module loads (so
// classes registered later in init are known) and a type that cannot be converted fails require()
// with a clear message instead of failing at call time.

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// defKind is what a funcDef implements.
type defKind uint8

const (
	kindFunc   defKind = iota // FuncN, ProcN
	kindAsync                 // AsyncN: arguments must outlive the call, result built later
	kindMethod                // MethodN: this is an instance of a class
	kindCtor                  // CtorN: returns the Go value of a new instance
)

// injectKind marks parameters filled from the call instead of from a JS argument.
type injectKind uint8

const (
	injectNone injectKind = iota
	injectEnv
	injectCallContext
)

var (
	envType         = reflect.TypeFor[Env]()
	callContextType = reflect.TypeFor[CallContext]()
)

type sigParam struct {
	t      reflect.Type
	c      *codec
	inject injectKind
	js     int // JS argument index (-1 when injected)
	// direct: decoded without an indirect call (primitives, napi value types).
	direct bool
}

// funcDef is the shared description behind FuncDef, MethodDef and CtorDef.
type funcDef struct {
	wrapper string // Func2, Method0E, ... for error messages
	kind    defKind
	types   []reflect.Type
	result  reflect.Type // nil when the function returns nothing to JS
	impl    func(cx call) (sys.Value, error)

	once    sync.Once
	params  []sigParam
	rc      *codec
	jsLen   int
	prepErr error
}

func newFuncDef(wrapper string, kind defKind, types []reflect.Type, result reflect.Type, impl func(cx call) (sys.Value, error)) *funcDef {
	return &funcDef{wrapper: wrapper, kind: kind, types: types, result: result, impl: impl}
}

// prepare resolves the codecs and checks that every type converts in the needed direction.
func (d *funcDef) prepare() error {
	d.once.Do(func() {
		async := d.kind == kindAsync
		d.params = make([]sigParam, len(d.types))
		for i, t := range d.types {
			p := sigParam{t: t, js: -1}
			switch t {
			case envType:
				p.inject = injectEnv
			case callContextType:
				p.inject = injectCallContext
			}
			if p.inject != injectNone {
				if async {
					d.prepErr = definitionError(fmt.Sprintf("%s: parameter %d", d.wrapper, i+1), unsupported(t, "cannot be used in a goroutine"))
					return
				}
				d.params[i] = p
				continue
			}
			p.js = d.jsLen
			d.jsLen++
			c := codecFor(t)
			if c.decode == nil {
				d.prepErr = definitionError(d.wrapper+": parameter "+strconv.Itoa(i+1), c.decodeErr)
				return
			}
			if async && c.borrows {
				d.prepErr = definitionError(fmt.Sprintf("%s: parameter %d", d.wrapper, i+1), unsupported(t, "refers to JS values and cannot be used in a goroutine"))
				return
			}
			p.c = c
			_, isValue := valueTypes[t]
			p.direct = c.prim != primNone || isValue
			d.params[i] = p
		}
		if d.result != nil {
			c := codecFor(d.result)
			if c.encode == nil {
				d.prepErr = definitionError(d.wrapper+": result", c.encodeErr)
				return
			}
			if async && c.borrows {
				d.prepErr = definitionError(d.wrapper+": result", unsupported(d.result, "refers to JS values and cannot be created in a goroutine"))
				return
			}
			d.rc = c
		}
	})
	return d.prepErr
}

// call is the state of one invocation, passed by value so that it stays on the stack.
type call struct {
	env  sys.Env
	info sys.CallbackInfo
	this sys.Value
	args []sys.Value
	d    *funcDef
	m    mode
	// Typed trampoline (typed.go): arguments converted by C, and the result slot C reads. nil on
	// the untyped path.
	slots []sys.TypedArg
	out   *sys.TypedOut
	// wrapped is the handle ID wrapped by this, read in C for methods (0: unknown or none).
	wrapped uintptr
}

func (cx *call) argValue(js int) Value {
	if js < len(cx.args) {
		return Value{env: cx.env, raw: cx.args[js]}
	}
	return Env{raw: cx.env}.Undefined()
}

// arg decodes parameter i. Primitives and napi values are decoded by direct calls, so the Go value
// stays on the stack; other types go through their codec.
func arg[T any](cx *call, i int) (T, error) {
	var v T
	p := &cx.d.params[i]
	switch {
	case p.inject == injectEnv:
		*(*Env)(unsafe.Pointer(&v)) = Env{raw: cx.env}
		return v, nil
	case p.inject == injectCallContext:
		*(*CallContext)(unsafe.Pointer(&v)) = CallContext{env: cx.env, info: cx.info, this: cx.this, args: cx.args}
		return v, nil
	}
	if p.c.prim != primNone && p.js < len(cx.slots) {
		if ok, err := slotPrim(&cx.slots[p.js], p.c.prim, unsafe.Pointer(&v)); ok {
			if err != nil {
				return v, finishPath("argument "+strconv.Itoa(p.js+1), err)
			}
			return v, nil
		}
	}
	jv := cx.argValue(p.js)
	var err error
	switch {
	case p.c.prim != primNone:
		err = decodePrim(jv, p.c.prim, unsafe.Pointer(&v), cx.m)
	case p.c.class != nil:
		err = decodeInstance(p.c.class, jv, unsafe.Pointer(&v))
	case p.direct:
		err = decodeValueType(p.t, jv, cx.m, unsafe.Pointer(&v))
	default:
		tmp := new(T)
		err = p.c.dec(jv, unsafe.Pointer(tmp), cx.m)
		v = *tmp
	}
	if err != nil {
		return v, finishPath("argument "+strconv.Itoa(p.js+1), err)
	}
	return v, nil
}

// decodeValueType decodes a napi value type (Value, Object, ...) without an indirect call taking p.
func decodeValueType(t reflect.Type, v Value, m mode, p unsafe.Pointer) error {
	vt := valueTypes[t]
	if m == modeOwn {
		return unsupported(t, "cannot be used outside the JS thread (Async)")
	}
	if vt.check != nil && !vt.check(v) {
		return convTypeError(vt.want, v)
	}
	*(*Value)(p) = v
	return nil
}

// result encodes the Go result r.
func result[T any](cx *call, r T) (sys.Value, error) {
	c := cx.d.rc
	var v Value
	var err error
	if c.prim != primNone && cx.out != nil && outPrim(cx.out, c.prim, unsafe.Pointer(&r)) {
		return nil, nil
	}
	switch {
	case c.prim != primNone:
		v, err = encodePrim(Env{raw: cx.env}, c.prim, unsafe.Pointer(&r))
	case c.class != nil:
		ptr := *(*unsafe.Pointer)(unsafe.Pointer(&r))
		if ptr == nil {
			v = Env{raw: cx.env}.Null()
		} else {
			v, err = instanceFor(Env{raw: cx.env}, c.class, ptr)
		}
	case isValueType(c.t):
		v = *(*Value)(unsafe.Pointer(&r))
		if v.raw == nil {
			return nil, nil
		}
	default:
		tmp := new(T)
		*tmp = r
		v, err = c.enc(Env{raw: cx.env}, unsafe.Pointer(tmp))
	}
	if err != nil {
		return nil, finishPath("result", err)
	}
	return v.raw, nil
}

func isValueType(t reflect.Type) bool {
	_, ok := valueTypes[t]
	return ok
}

// resolveWith returns the Resolver that encodes the result of an Async function on the JS thread.
func resolveWith[T any](d *funcDef, r T) Resolver {
	return func(env Env) (Value, error) {
		if d.rc == nil {
			return Value{}, nil
		}
		v, err := d.rc.enc(env, unsafe.Pointer(&r))
		return v, finishPath("result", err)
	}
}

// startAsync runs work in a goroutine and returns its Promise (Env.Async).
func startAsync(cx *call, work func(ctx context.Context) (Resolver, error)) (sys.Value, error) {
	p, err := Env{raw: cx.env}.Async(work)
	if err != nil {
		return nil, err
	}
	return p.raw, nil
}

// ---- FuncDef ----

// FuncDef is a Go function exposed to JS, with automatic conversion of its arguments and result
// (see Func1 and the other wrappers). Export it with Export, use it as a static class member, or
// create JS functions from it with New.
type FuncDef struct{ d *funcDef }

// New creates a JS function named name that runs f. It must be called on the JS thread.
func (f FuncDef) New(env Env, name string) (Function, error) {
	if f.d == nil {
		return Function{}, TypeError("", "FuncDef.New: zero FuncDef")
	}
	if err := f.d.prepare(); err != nil {
		return Function{}, err
	}
	fn, err := env.newTypedFunction(name, f.d)
	if err != nil {
		return Function{}, err
	}
	// length counts the JS parameters (injected ones excluded), as for a JS function; Node-API
	// leaves it at 0. Like the built-in property it is configurable and read-only.
	n, err := env.NewInt32(int32(f.d.jsLen))
	if err != nil {
		return Function{}, err
	}
	return fn, fn.DefineProperties(Property{Name: "length", Value: n, Attributes: Configurable})
}

func (f FuncDef) materialize(env Env, name string) (Value, error) {
	fn, err := f.New(env, name)
	return fn.Value, err
}

// ---- export registry ----

// Exportable is a definition that Export can materialize in every env: a FuncDef, a ClassDef or
// a Const.
type Exportable interface {
	materialize(env Env, name string) (Value, error)
}

type exportEntry struct {
	name   string
	def    Exportable
	export bool
}

var exportRegistry struct {
	mu      sync.Mutex
	entries []exportEntry
	names   map[string]bool
	hooked  bool
}

// Export adds name to the module exports, in every env that loads the addon (main thread and
// worker_threads). Call it from init(). The exports are created in registration order, together
// with the functions registered with Init: the first Export call takes the place of an Init call.
// A type that cannot be converted makes require() fail with a message naming the export.
func Export(name string, def Exportable) {
	if def == nil {
		panic("napi: Export(" + strconv.Quote(name) + ", nil)")
	}
	if c, ok := def.(interface{ registerType(name string) }); ok {
		c.registerType(name) // a class: its *T converts to its instances
	}
	register(exportEntry{name: name, def: def, export: true})
}

func register(e exportEntry) {
	exportRegistry.mu.Lock()
	defer exportRegistry.mu.Unlock()
	if e.export {
		if exportRegistry.names[e.name] {
			panic("napi: duplicate export " + strconv.Quote(e.name))
		}
		if exportRegistry.names == nil {
			exportRegistry.names = map[string]bool{}
		}
		exportRegistry.names[e.name] = true
	}
	exportRegistry.entries = append(exportRegistry.entries, e)
	if !exportRegistry.hooked {
		exportRegistry.hooked = true
		Init(materializeExports)
	}
}

func materializeExports(env Env, exports Object) (Value, error) {
	exportRegistry.mu.Lock()
	entries := exportRegistry.entries
	exportRegistry.mu.Unlock()
	for _, e := range entries {
		v, err := e.def.materialize(env, e.name)
		if err != nil {
			return Value{}, exportError(e.name, err)
		}
		if e.export {
			if err := exports.Set(e.name, v); err != nil {
				return Value{}, err
			}
		}
	}
	return Value{}, nil
}

// exportError names the export in a materialization error, keeping the JS kind and code.
func exportError(name string, err error) error {
	if ne, ok := err.(*Error); ok {
		if ne.Status == StatusPendingException {
			return err
		}
		return &Error{Status: ne.Status, Kind: ne.Kind, Code: ne.Code, Message: "export " + strconv.Quote(name) + ": " + ne.Message}
	}
	return fmt.Errorf("export %q: %w", name, err)
}

// Const exports a value converted with ToValue, created once per env.
func Const(v any) Exportable { return constDef{v} }

type constDef struct{ v any }

func (c constDef) materialize(env Env, _ string) (Value, error) { return ToValue(env, c.v) }
