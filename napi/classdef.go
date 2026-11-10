package napi

// classdef.go — Layer 2: classes backed by a Go type.

import (
	"reflect"
	"strconv"
	"sync"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// MethodDef is a method of the class of T (see Method0 and the other Method wrappers).
type MethodDef[T any] struct{ d *funcDef }

// CtorDef is the constructor of the class of T (see Ctor0 and the other Ctor wrappers).
type CtorDef[T any] struct {
	d    *funcDef
	make func(cx call) (*T, error)
}

// AccessorDef is a property of the instances of the class of T backed by a getter and an optional
// setter (see Accessor).
type AccessorDef[T any] struct{ get, set *funcDef }

// Accessor defines a property read with get and, if set is not nil, written with set.
func Accessor[T, V any](get func(*T) V, set func(*T, V)) AccessorDef[T] {
	if get == nil {
		panic("napi: Accessor with a nil getter")
	}
	var s func(*T, V) error
	if set != nil {
		s = func(t *T, v V) error { set(t, v); return nil }
	}
	return AccessorE(func(t *T) (V, error) { return get(t), nil }, s)
}

// AccessorE is Accessor with a getter and setter that can fail; errors are thrown in JS.
func AccessorE[T, V any](get func(*T) (V, error), set func(*T, V) error) AccessorDef[T] {
	if get == nil {
		panic("napi: AccessorE with a nil getter")
	}
	a := AccessorDef[T]{get: MethodE0(get).d}
	if set != nil {
		a.set = MethodProcE1(set).d
	}
	return a
}

// receiver returns the Go value of this for a method of the class of T.
func receiver[T any](cx *call) (*T, error) {
	var v any
	var err error
	if cx.wrapped != 0 {
		// Read by the typed trampoline: no Node-API call from Go.
		if h := handles.get(cx.wrapped); h != nil {
			v = h.value
		} else {
			err = ErrNotWrapped
		}
	} else {
		v, err = Object{Value{env: cx.env, raw: cx.this}}.Unwrap()
	}
	if err == nil {
		if r, ok := v.(*T); ok && r != nil {
			return r, nil
		}
	}
	return nil, &Error{Kind: KindTypeError, Code: CodeInvalidArgType,
		Message: "Illegal invocation: this is not an instance of " + classNameOf(reflect.TypeFor[T]())}
}

// ---- class definition ----

type classMember[T any] struct {
	name     string
	method   *funcDef
	accessor AccessorDef[T]
	static   *funcDef
	value    any
	isValue  bool
}

// ClassDef defines a JS class whose instances wrap a *T. Build it with NewClass and its methods,
// then Export it (or RegisterClass it). A *T parameter or result of any wrapper converts to and
// from the instances of the class: returning a *T creates its instance (or returns the existing
// one, so the same *T is always the same JS object in an env), and a *T parameter accepts an
// instance (or null/undefined as nil).
//
// If *T has a method Finalize(env Env), it is called when the last instance wrapping that *T is
// garbage-collected (or its env destroyed), on the JS thread; it must not call into JS.
type ClassDef[T any] struct {
	ctor    CtorDef[T]
	members []classMember[T]
	names   map[string]bool
}

// NewClass starts a class definition with its constructor. With the zero CtorDef, the class
// cannot be instantiated from JS (new throws a TypeError): its instances come from Go results.
func NewClass[T any](ctor CtorDef[T]) *ClassDef[T] {
	return &ClassDef[T]{ctor: ctor, names: map[string]bool{}}
}

func (c *ClassDef[T]) add(m classMember[T]) *ClassDef[T] {
	key := m.name
	if m.static != nil || m.isValue {
		key = "static " + key
	}
	if c.names[key] {
		panic("napi: duplicate class member " + strconv.Quote(m.name))
	}
	c.names[key] = true
	c.members = append(c.members, m)
	return c
}

// Method adds a method on the prototype.
func (c *ClassDef[T]) Method(name string, m MethodDef[T]) *ClassDef[T] {
	if m.d == nil {
		panic("napi: Method " + strconv.Quote(name) + " with a zero MethodDef")
	}
	return c.add(classMember[T]{name: name, method: m.d})
}

// Accessor adds a property on the prototype (configurable, not enumerable, like a JS class
// getter/setter).
func (c *ClassDef[T]) Accessor(name string, a AccessorDef[T]) *ClassDef[T] {
	if a.get == nil {
		panic("napi: Accessor " + strconv.Quote(name) + " with a zero AccessorDef")
	}
	return c.add(classMember[T]{name: name, accessor: a})
}

// Static adds a static method on the constructor.
func (c *ClassDef[T]) Static(name string, f FuncDef) *ClassDef[T] {
	if f.d == nil {
		panic("napi: Static " + strconv.Quote(name) + " with a zero FuncDef")
	}
	return c.add(classMember[T]{name: name, static: f.d})
}

// StaticConst adds a read-only static property holding v (converted with ToValue).
func (c *ClassDef[T]) StaticConst(name string, v any) *ClassDef[T] {
	return c.add(classMember[T]{name: name, value: v, isValue: true})
}

// RegisterClass makes the class known to every env without exporting it: its instances can only
// be created by Go results. name is the class name seen by JS.
func RegisterClass[T any](name string, c *ClassDef[T]) {
	if c == nil {
		panic("napi: RegisterClass(" + strconv.Quote(name) + ", nil)")
	}
	c.registerType(name)
	register(exportEntry{name: name, def: c})
}

func (c *ClassDef[T]) registerType(name string) {
	t := reflect.TypeFor[T]()
	classes.mu.Lock()
	defer classes.mu.Unlock()
	if prev, dup := classes.byType[t]; dup {
		panic("napi: type " + t.String() + " is already the class " + strconv.Quote(prev.name))
	}
	if classes.byType == nil {
		classes.byType = map[reflect.Type]*classMeta{}
	}
	classes.byType[t] = &classMeta{t: t, ptr: reflect.PointerTo(t), name: name}
}

// classMeta is what the codecs need to know about a class.
type classMeta struct {
	t, ptr reflect.Type // T and *T
	name   string
}

var classes struct {
	mu     sync.Mutex
	byType map[reflect.Type]*classMeta
}

func classOf(t reflect.Type) *classMeta {
	classes.mu.Lock()
	defer classes.mu.Unlock()
	return classes.byType[t]
}

func classNameOf(t reflect.Type) string {
	if m := classOf(t); m != nil {
		return m.name
	}
	return t.String()
}

func (c *ClassDef[T]) prepare() error {
	if c.ctor.d != nil {
		if err := c.ctor.d.prepare(); err != nil {
			return err
		}
	}
	for _, m := range c.members {
		for _, d := range []*funcDef{m.method, m.static, m.accessor.get, m.accessor.set} {
			if d == nil {
				continue
			}
			if err := d.prepare(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *ClassDef[T]) materialize(env Env, name string) (Value, error) {
	if err := c.prepare(); err != nil {
		return Value{}, err
	}
	props := make([]Property, 0, len(c.members))
	for _, m := range c.members {
		p := Property{Name: m.name}
		switch {
		case m.method != nil:
			p.typed, p.typedKinds, p.Attributes = &typedEntry{fn: m.method}, m.method, DefaultMethod
		case m.static != nil:
			p.typed, p.typedKinds, p.Attributes = &typedEntry{fn: m.static}, m.static, DefaultMethod|Static
		case m.isValue:
			v, err := ToValue(env, m.value)
			if err != nil {
				return Value{}, finishPath("static "+m.name, err)
			}
			p.Value, p.Attributes = v, Enumerable|Static
		default:
			// Getter and setter share the descriptor data: C converts the setter's argument.
			p.typed = &typedEntry{get: m.accessor.get, set: m.accessor.set}
			p.typedKinds, p.Attributes = m.accessor.set, Configurable
		}
		props = append(props, p)
	}
	cls, err := env.DefineClass(name, c.constructor(name), props...)
	if err != nil {
		return Value{}, err
	}
	st, err := env.state()
	if err != nil {
		return Value{}, err
	}
	ref, err := cls.NewRef(1)
	if err != nil {
		return Value{}, err
	}
	if st.classes == nil {
		st.classes = map[reflect.Type]Ref{}
	}
	if old, ok := st.classes[reflect.TypeFor[T]()]; ok {
		// The class was already materialized in this env (Init ran again): keep the newest
		// constructor. Delete fails only for a destroyed env, whose references Node frees.
		_ = old.Delete()
	}
	st.classes[reflect.TypeFor[T]()] = ref
	return cls.Value, nil
}

// instanceSeed is the hidden argument with which Go creates an instance of a class around an
// existing *T. JS cannot create Externals, and this type is private: it cannot be forged.
type instanceSeed struct{ p unsafe.Pointer }

func (c *ClassDef[T]) constructor(name string) Callback {
	return func(cc CallContext) (Value, error) {
		this := cc.This()
		if len(cc.args) == 1 {
			if a := cc.Arg(0); a.IsExternal() {
				if x, err := (External{a}).Get(); err == nil {
					if seed, ok := x.(*instanceSeed); ok {
						return Value{}, attachInstance(cc.Env(), this, (*T)(seed.p))
					}
				}
			}
		}
		if c.ctor.d == nil {
			return Value{}, TypeError("", "Class "+name+" cannot be constructed from JS")
		}
		v, err := c.ctor.make(call{env: cc.env, info: cc.info, this: cc.this, args: cc.args, d: c.ctor.d})
		if err != nil {
			return Value{}, err
		}
		if v == nil {
			return Value{}, &Error{Code: sys.CodeInternal, Message: "napi-go: constructor of " + name + " returned a nil *" + reflect.TypeFor[T]().String()}
		}
		return Value{}, attachInstance(cc.Env(), this, v)
	}
}

// instEntry tracks the JS object wrapping a *T in an env: ref is weak (count 0), wrappers counts
// the objects still wrapping the pointer (more than one only when an object was collected but
// not yet finalized when Go returned the same *T again).
type instEntry struct {
	ref      Ref
	wrappers int
}

// finalizer is implemented by class types that release resources with their last instance.
type finalizer interface{ Finalize(env Env) }

func attachInstance[T any](env Env, this Object, v *T) error {
	st, err := env.state()
	if err != nil {
		return err
	}
	key := unsafe.Pointer(v)
	if err := this.Wrap(v, func(env Env, _ any) { releaseInstance(env, st, key, v) }); err != nil {
		return err
	}
	ref, err := this.NewRef(0)
	if err != nil {
		return err
	}
	if st.instances == nil {
		st.instances = map[unsafe.Pointer]*instEntry{}
	}
	e := st.instances[key]
	if e == nil {
		e = &instEntry{}
		st.instances[key] = e
	} else if !e.ref.IsEmpty() {
		// The previous object is gone (only its finalizer is pending): drop its weak reference.
		// Delete fails only for a destroyed env, whose references Node frees itself.
		_ = e.ref.Delete()
	}
	e.ref = ref
	e.wrappers++
	return nil
}

func releaseInstance[T any](env Env, st *envState, key unsafe.Pointer, v *T) {
	e := st.instances[key]
	if e == nil {
		return
	}
	e.wrappers--
	if e.wrappers > 0 {
		return
	}
	delete(st.instances, key)
	// Fails only for a destroyed env, whose references Node frees itself.
	_ = e.ref.Delete()
	if f, ok := any(v).(finalizer); ok {
		f.Finalize(env)
	}
}

// fillClassPointer: *T of a registered class converts to and from its instances.
func fillClassPointer(c *codec, t reflect.Type) bool {
	meta := classOf(t.Elem())
	if meta == nil {
		return false
	}
	c.optional = true
	c.class = meta
	c.jsName = "an instance of " + meta.name + " or null"
	c.decode = func(v Value, p unsafe.Pointer, _ mode) error { return decodeInstance(meta, v, p) }
	c.encode = func(env Env, p unsafe.Pointer) (Value, error) {
		ptr := *(*unsafe.Pointer)(p)
		if ptr == nil {
			return env.Null(), nil
		}
		return instanceFor(env, meta, ptr)
	}
	return true
}

// decodeInstance stores at p the *T wrapped by the instance v (nil for null or undefined). p does
// not escape.
func decodeInstance(meta *classMeta, v Value, p unsafe.Pointer) error {
	if v.IsNullish() {
		*(*unsafe.Pointer)(p) = nil
		return nil
	}
	if v.IsObject() {
		if x, err := (Object{v}).Unwrap(); err == nil {
			// The wrapped value is a *T (any T): compare its dynamic type with the class type.
			if ptr, ok := pointerOf(x, meta.ptr); ok {
				*(*unsafe.Pointer)(p) = ptr
				return nil
			}
		}
	}
	return convTypeError("an instance of "+meta.name, v)
}

// pointerOf returns the pointer held by x if x is a non-nil value of type ptrType.
func pointerOf(x any, ptrType reflect.Type) (unsafe.Pointer, bool) {
	rv := reflect.ValueOf(x)
	if !rv.IsValid() || rv.Type() != ptrType || rv.IsNil() {
		return nil, false
	}
	return rv.UnsafePointer(), true
}

// instanceFor returns the JS object wrapping ptr in env, creating it if needed.
func instanceFor(env Env, meta *classMeta, ptr unsafe.Pointer) (Value, error) {
	st, err := env.state()
	if err != nil {
		return Value{}, err
	}
	if e := st.instances[ptr]; e != nil {
		v, err := e.ref.Value()
		if err != nil {
			return Value{}, err
		}
		if !v.IsEmpty() {
			return v, nil
		}
	}
	ref, ok := st.classes[meta.t]
	if !ok {
		return Value{}, &Error{Code: sys.CodeInternal, Message: "napi-go: class " + meta.name + " is not defined in this env"}
	}
	cv, err := ref.Value()
	if err != nil {
		return Value{}, err
	}
	seed, err := env.NewExternal(&instanceSeed{p: ptr}, nil)
	if err != nil {
		return Value{}, err
	}
	obj, err := (Function{Object{cv}}).New(seed.Value)
	return obj.Value, err
}
