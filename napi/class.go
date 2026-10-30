package napi

import (
	"errors"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// DefineClass crea una classe JS (napi_define_class). constructor viene chiamata con new: il this
// del CallContext è l'istanza, a cui di solito si associa un valore Go con Wrap. Le proprietà
// con Static vanno sul costruttore, le altre sul prototipo. Le callback restano registrate finché
// il costruttore vive.
//
// Chiamare la classe senza new lancia un TypeError, come per le classi JS.
func (e Env) DefineClass(name string, constructor Callback, props ...Property) (Function, error) {
	if constructor == nil {
		return Function{}, TypeError("", "DefineClass: nil constructor")
	}
	descs, ids, err := e.descriptors(props)
	if err != nil {
		return Function{}, err
	}
	ctorID := registerCallback(requireNew(name, constructor))
	ids = append(ids, ctorID)
	var descp *sys.PropertyDescriptor
	if len(descs) > 0 {
		descp = &descs[0]
	}
	var r sys.Value
	st := sys.DefineClass(e.raw, unsafe.StringData(name), uint(len(name)), sys.CallbackTrampoline, ctorID,
		uint(len(descs)), descp, &r)
	v, err := e.value(r, st)
	if err != nil {
		releaseCallbacks(ids)
		return Function{}, err
	}
	f := Function{Object{v}}
	if err := e.releaseCallbacksWith(f.Object, ids); err != nil {
		return Function{}, err
	}
	return f, nil
}

// requireNew rifiuta le chiamate senza new: Node-API, a differenza delle classi JS, le
// accetterebbe con un this sbagliato.
func requireNew(name string, fn Callback) Callback {
	return func(c CallContext) (Value, error) {
		if !c.IsConstructCall() {
			return Value{}, TypeError("", "Class constructor "+name+" cannot be invoked without 'new'")
		}
		return fn(c)
	}
}

// ---- Promise risolte sul thread JS ----

// Promise è una Promise JS.
type Promise struct{ Object }

// Deferred risolve o rifiuta la Promise creata da NewPromise (napi_deferred). Si usa una volta
// sola e solo sul thread JS dell'env; per risolvere da una goroutine servono le API asincrone
// (Fase 3).
type Deferred struct {
	env sys.Env
	raw sys.Deferred
}

// NewPromise crea una Promise in attesa e il Deferred che la conclude.
func (e Env) NewPromise() (Promise, Deferred, error) {
	var d sys.Deferred
	var r sys.Value
	st := sys.CreatePromise(e.raw, &d, &r)
	v, err := e.value(r, st)
	if err != nil {
		return Promise{}, Deferred{}, err
	}
	return Promise{Object{v}}, Deferred{env: e.raw, raw: d}, nil
}

// Resolve risolve la Promise con v.
func (d Deferred) Resolve(v Value) error {
	return check(d.env, sys.ResolveDeferred(d.env, d.raw, v.raw))
}

// Reject rifiuta la Promise con v.
func (d Deferred) Reject(v Value) error {
	return check(d.env, sys.RejectDeferred(d.env, d.raw, v.raw))
}

// RejectError rifiuta la Promise con l'Error JS che rappresenta err (stesse regole di una
// callback che restituisce err).
func (d Deferred) RejectError(err error) error {
	if err == nil {
		return TypeError("", "RejectError: nil error")
	}
	var thrown *Thrown
	if errors.As(err, &thrown) && thrown.Value.raw != nil {
		return d.Reject(thrown.Value)
	}
	v, cerr := newErrorValue(d.env, err)
	if cerr != nil {
		return cerr
	}
	return check(d.env, sys.RejectDeferred(d.env, d.raw, v))
}
