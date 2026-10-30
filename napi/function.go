package napi

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Callback è una funzione Go chiamabile da JS. Un errore restituito viene lanciato in JS (vedi
// Error); un Value vuoto equivale a undefined.
type Callback func(c CallContext) (Value, error)

// CallContext descrive una chiamata da JS (napi_callback_info, Napi::CallbackInfo). Gli argomenti
// sono validi solo durante la callback. Si passa per valore: è piccolo e così non finisce sullo
// heap.
type CallContext struct {
	env  sys.Env
	info sys.CallbackInfo
	this sys.Value
	args []sys.Value
}

// Env restituisce l'env della chiamata.
func (c CallContext) Env() Env { return Env{raw: c.env} }

// This restituisce il this della chiamata (per un costruttore, l'oggetto in costruzione).
func (c CallContext) This() Object { return Object{Value{env: c.env, raw: c.this}} }

// Len restituisce il numero di argomenti passati.
func (c CallContext) Len() int { return len(c.args) }

// Arg restituisce l'argomento i-esimo, undefined se non è stato passato.
func (c CallContext) Arg(i int) Value {
	if i >= 0 && i < len(c.args) {
		return Value{env: c.env, raw: c.args[i]}
	}
	return Env{raw: c.env}.Undefined()
}

// Args aggiunge gli argomenti a dst e restituisce lo slice risultante.
func (c CallContext) Args(dst []Value) []Value {
	for _, a := range c.args {
		dst = append(dst, Value{env: c.env, raw: a})
	}
	return dst
}

// NewTarget restituisce new.target: vuoto se la funzione non è chiamata con new.
func (c CallContext) NewTarget() (Value, error) {
	var r sys.Value
	if err := check(c.env, sys.GetNewTarget(c.env, c.info, &r)); err != nil {
		return Value{}, err
	}
	return Value{env: c.env, raw: r}, nil
}

// IsConstructCall dice se la funzione è chiamata con new.
func (c CallContext) IsConstructCall() bool {
	t, err := c.NewTarget()
	return err == nil && t.raw != nil
}

// callbacks contiene le Callback raggiungibili da JS: il data di ogni funzione creata da napi è
// un id di questa tabella.
var callbacks table[Callback]

func registerCallback(fn Callback) uintptr { return callbacks.add(&fn) }

func releaseCallbacks(ids []uintptr) {
	for _, id := range ids {
		callbacks.remove(id)
	}
}

// releaseCallbacksWith rilascia ids quando o viene raccolto dal GC (o l'env distrutto). Se
// l'aggancio del finalizer fallisce, gli id vengono rilasciati subito: o resta senza callback
// valide, ma la memoria non si perde.
func (e Env) releaseCallbacksWith(o Object, ids []uintptr) error {
	if len(ids) == 0 {
		return nil
	}
	err := o.AddFinalizer(func(Env) { releaseCallbacks(ids) })
	if err != nil {
		releaseCallbacks(ids)
	}
	return err
}

// dispatchCallback è il CallbackHandler di sys per tutte le funzioni create da napi.
func dispatchCallback(env sys.Env, info sys.CallbackInfo, data uintptr, this sys.Value, args []sys.Value) sys.Value {
	fn := callbacks.get(data)
	if fn == nil {
		throwError(env, &Error{Code: sys.CodeInternal, Message: "napi-go: callback not registered"})
		return nil
	}
	v, err := (*fn)(CallContext{env: env, info: info, this: this, args: args})
	if err != nil {
		throwError(env, err)
		return nil
	}
	return v.raw
}

// accessor unisce getter e setter in una callback: Node-API chiama il getter senza argomenti e il
// setter con il valore assegnato.
func accessor(get, set Callback) Callback {
	return func(c CallContext) (Value, error) {
		if c.Len() == 0 {
			if get == nil {
				return Value{}, nil
			}
			return get(c)
		}
		return set(c)
	}
}

// Function è una funzione JS.
type Function struct{ Object }

// NewFunction crea una funzione JS con il nome dato che chiama fn. fn resta registrata finché la
// funzione vive.
func (e Env) NewFunction(name string, fn Callback) (Function, error) {
	if fn == nil {
		return Function{}, TypeError("", "NewFunction: nil callback")
	}
	id := registerCallback(fn)
	var r sys.Value
	st := sys.CreateFunction(e.raw, unsafe.StringData(name), uint(len(name)), sys.CallbackTrampoline, id, &r)
	v, err := e.value(r, st)
	if err != nil {
		callbacks.remove(id)
		return Function{}, err
	}
	f := Function{Object{v}}
	if err := e.releaseCallbacksWith(f.Object, []uintptr{id}); err != nil {
		return Function{}, err
	}
	return f, nil
}

// rawArgs converte args nei napi_value da passare a Node-API. Il buffer finisce sullo heap (lo
// usano funzioni che eseguono JS, quindi senza nocallback): Call e New lo creano solo se ci sono
// argomenti, e della dimensione esatta.
func rawArgs(args []Value) []sys.Value {
	raw := make([]sys.Value, len(args))
	for i, a := range args {
		raw[i] = a.raw
	}
	return raw
}

// Call chiama la funzione con this e gli argomenti dati; un this vuoto vale undefined. Senza
// argomenti non alloca; con argomenti alloca il loro buffer. Un'eccezione JS dà
// ErrPendingException.
func (f Function) Call(this Value, args ...Value) (Value, error) {
	recv := this.raw
	if recv == nil {
		recv = Env{raw: f.env}.Undefined().raw
	}
	var r sys.Value
	var st sys.Status
	if len(args) == 0 {
		st = sys.CallFunction(f.env, recv, f.raw, 0, nil, &r)
	} else {
		raw := rawArgs(args)
		st = sys.CallFunction(f.env, recv, f.raw, uint(len(raw)), &raw[0], &r)
	}
	return Env{raw: f.env}.value(r, st)
}

// New chiama la funzione come costruttore (new f(...args)), con lo stesso costo di Call.
func (f Function) New(args ...Value) (Object, error) {
	var r sys.Value
	var st sys.Status
	if len(args) == 0 {
		st = sys.NewInstance(f.env, f.raw, 0, nil, &r)
	} else {
		raw := rawArgs(args)
		st = sys.NewInstance(f.env, f.raw, uint(len(raw)), &raw[0], &r)
	}
	v, err := Env{raw: f.env}.value(r, st)
	return Object{v}, err
}
