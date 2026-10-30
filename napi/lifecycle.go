package napi

import (
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// ---- finalizer ----

// AddFinalizer fa chiamare fn quando o viene raccolto dal GC di V8 o l'env viene distrutto
// (napi_add_finalizer). Vedi FinalizeFunc per i vincoli su ciò che fn può fare.
func (o Object) AddFinalizer(fn func(env Env)) error {
	if fn == nil {
		return TypeError("", "AddFinalizer: nil function")
	}
	id := handles.add(&handle{onFinalize: func(e Env, _ any) { fn(e) }})
	if err := check(o.env, sys.AddFinalizer(o.env, o.raw, 0, sys.FinalizeTrampoline, id, nil)); err != nil {
		handles.remove(id)
		return err
	}
	return nil
}

// ---- wrap ----

// ErrNotWrapped è restituito da Unwrap e RemoveWrap per un oggetto senza valore Go associato.
var ErrNotWrapped = &Error{Status: StatusInvalidArg, Kind: KindTypeError, Message: "object does not wrap a Go value"}

// Wrap associa il valore Go v all'oggetto JS o (napi_wrap): Unwrap lo restituisce finché o vive.
// fin, se non nil, viene chiamata quando o viene raccolto. Un oggetto può avere un solo valore
// associato.
func (o Object) Wrap(v any, fin FinalizeFunc) error {
	id := handles.add(&handle{value: v, onFinalize: fin})
	if err := check(o.env, sys.Wrap(o.env, o.raw, id, sys.FinalizeTrampoline, id, nil)); err != nil {
		handles.remove(id)
		return err
	}
	return nil
}

// Unwrap restituisce il valore associato con Wrap.
func (o Object) Unwrap() (any, error) {
	var p unsafe.Pointer
	if err := check(o.env, sys.Unwrap(o.env, o.raw, &p)); err != nil {
		if ne, ok := err.(*Error); ok && ne.Status == StatusInvalidArg {
			return nil, ErrNotWrapped
		}
		return nil, err
	}
	h := handles.get(uintptr(p))
	if h == nil {
		return nil, ErrNotWrapped
	}
	return h.value, nil
}

// RemoveWrap rimuove l'associazione e restituisce il valore. Il finalizer di Wrap non verrà più
// chiamato.
func (o Object) RemoveWrap() (any, error) {
	var p unsafe.Pointer
	if err := check(o.env, sys.RemoveWrap(o.env, o.raw, &p)); err != nil {
		if ne, ok := err.(*Error); ok && ne.Status == StatusInvalidArg {
			return nil, ErrNotWrapped
		}
		return nil, err
	}
	h := handles.remove(uintptr(p))
	if h == nil {
		return nil, ErrNotWrapped
	}
	return h.value, nil
}

// UnwrapAs restituisce il valore associato a o con Wrap convertito a T; un tipo diverso dà un
// TypeError.
func UnwrapAs[T any](o Object) (T, error) {
	var zero T
	v, err := o.Unwrap()
	if err != nil {
		return zero, err
	}
	t, ok := v.(T)
	if !ok {
		return zero, TypeError("", "wrapped Go value has a different type")
	}
	return t, nil
}

// TypeTag è un'etichetta di 128 bit da associare a un oggetto (napi_type_tag), per riconoscere in
// modo sicuro gli oggetti creati dal proprio addon prima di Unwrap.
type TypeTag = sys.TypeTag

// SetTypeTag associa tag a o; un oggetto può riceverne uno solo (Node-API 8).
func (o Object) SetTypeTag(tag TypeTag) error {
	return check(o.env, sys.TypeTagObject(o.env, o.raw, &tag))
}

// HasTypeTag dice se o ha l'etichetta tag.
func (o Object) HasTypeTag(tag TypeTag) (bool, error) {
	var b bool
	st := sys.CheckObjectTypeTag(o.env, o.raw, &tag, &b)
	return b, check(o.env, st)
}

// ---- external ----

// External è un valore JS opaco che porta con sé un valore Go (napi_create_external).
type External struct{ Value }

// NewExternal crea un external che contiene v. fin, se non nil, viene chiamata quando l'external
// viene raccolto.
func (e Env) NewExternal(v any, fin FinalizeFunc) (External, error) {
	id := handles.add(&handle{value: v, onFinalize: fin})
	var r sys.Value
	st := sys.CreateExternal(e.raw, id, sys.FinalizeTrampoline, id, &r)
	val, err := e.value(r, st)
	if err != nil {
		handles.remove(id)
		return External{}, err
	}
	return External{val}, nil
}

// Get restituisce il valore Go dell'external.
func (x External) Get() (any, error) {
	var p unsafe.Pointer
	if err := check(x.env, sys.GetValueExternal(x.env, x.raw, &p)); err != nil {
		return nil, err
	}
	h := handles.get(uintptr(p))
	if h == nil {
		return nil, TypeError("", "external was not created by napi-go")
	}
	return h.value, nil
}

// ---- reference ----

// Ref tiene un riferimento a un valore JS oltre lo handle scope corrente (napi_ref). Con conteggio
// > 0 il valore non viene raccolto; con conteggio 0 il riferimento è debole e Value può restituire
// un Value vuoto. Una Ref si usa solo sul thread JS del suo env e va rilasciata con Delete.
type Ref struct {
	env sys.Env
	raw sys.Ref
}

// NewRef crea un riferimento a v con il conteggio iniziale dato. Con Node-API ≤ 9 sono ammessi
// solo oggetti, funzioni, external e symbol; dalla 10 qualsiasi valore.
func (v Value) NewRef(count uint32) (Ref, error) {
	var r sys.Ref
	if err := check(v.env, sys.CreateReference(v.env, v.raw, count, &r)); err != nil {
		return Ref{}, err
	}
	return Ref{env: v.env, raw: r}, nil
}

// IsEmpty dice se r è la Ref zero.
func (r Ref) IsEmpty() bool { return r.raw == nil }

// Value restituisce il valore riferito; vuoto se un riferimento debole è stato raccolto.
func (r Ref) Value() (Value, error) {
	var v sys.Value
	if err := check(r.env, sys.GetReferenceValue(r.env, r.raw, &v)); err != nil {
		return Value{}, err
	}
	return Value{env: r.env, raw: v}, nil
}

// Ref incrementa il conteggio e lo restituisce.
func (r Ref) Ref() (uint32, error) {
	var n uint32
	st := sys.ReferenceRef(r.env, r.raw, &n)
	return n, check(r.env, st)
}

// Unref decrementa il conteggio e lo restituisce; a 0 il riferimento diventa debole.
func (r Ref) Unref() (uint32, error) {
	var n uint32
	st := sys.ReferenceUnref(r.env, r.raw, &n)
	return n, check(r.env, st)
}

// Delete rilascia il riferimento. Dopo Delete la Ref non va più usata.
func (r Ref) Delete() error { return check(r.env, sys.DeleteReference(r.env, r.raw)) }

// ---- handle scope ----

// WithScope esegue fn in un nuovo handle scope (napi_open_handle_scope / napi_close_handle_scope):
// i Value creati dentro fn vengono rilasciati alla fine. Serve nei cicli che creano molti valori.
// I Value creati in fn non vanno usati dopo.
func (e Env) WithScope(fn func() error) error {
	var s sys.HandleScope
	if err := check(e.raw, sys.OpenHandleScope(e.raw, &s)); err != nil {
		return err
	}
	ferr := fn()
	if err := check(e.raw, sys.CloseHandleScope(e.raw, s)); err != nil && ferr == nil {
		return err
	}
	return ferr
}

// WithEscapableScope esegue fn in un handle scope da cui il Value restituito da fn sopravvive
// (napi_escape_handle).
func (e Env) WithEscapableScope(fn func() (Value, error)) (Value, error) {
	var s sys.EscapableHandleScope
	if err := check(e.raw, sys.OpenEscapableHandleScope(e.raw, &s)); err != nil {
		return Value{}, err
	}
	v, ferr := fn()
	var escaped sys.Value
	if ferr == nil && v.raw != nil {
		ferr = check(e.raw, sys.EscapeHandle(e.raw, s, v.raw, &escaped))
	}
	if err := check(e.raw, sys.CloseEscapableHandleScope(e.raw, s)); err != nil && ferr == nil {
		return Value{}, err
	}
	if ferr != nil {
		return Value{}, ferr
	}
	return Value{env: e.raw, raw: escaped}, nil
}

// ---- cleanup hook ----

// CleanupHook è un hook registrato con AddCleanupHook.
type CleanupHook struct {
	env sys.Env
	id  uintptr
	h   *handle // per riconoscere l'id dopo che è stato liberato e riassegnato
}

// AddCleanupHook fa chiamare fn quando l'env viene distrutto (uscita del processo o fine del
// worker), prima dei finalizer. Gli hook girano in ordine inverso di registrazione.
func (e Env) AddCleanupHook(fn func()) (CleanupHook, error) {
	if fn == nil {
		return CleanupHook{}, TypeError("", "AddCleanupHook: nil function")
	}
	h := &handle{value: fn}
	id := handles.add(h)
	if err := check(e.raw, sys.AddEnvCleanupHook(e.raw, sys.CleanupHookTrampoline, id)); err != nil {
		handles.remove(id)
		return CleanupHook{}, err
	}
	return CleanupHook{env: e.raw, id: id, h: h}, nil
}

// Remove annulla l'hook se non è ancora stato eseguito.
func (h CleanupHook) Remove() error {
	if h.h == nil || handles.get(h.id) != h.h {
		return nil // già eseguito o già rimosso
	}
	if err := check(h.env, sys.RemoveEnvCleanupHook(h.env, sys.CleanupHookTrampoline, h.id)); err != nil {
		return err
	}
	handles.remove(h.id)
	return nil
}

func dispatchCleanupHook(arg uintptr) {
	h := handles.remove(arg)
	if h == nil {
		return
	}
	if fn, ok := h.value.(func()); ok {
		fn()
	}
}
