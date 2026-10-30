package napi

import (
	"github.com/napi-bindings/napi-go/sys"
)

// Object è un oggetto JS (comprese funzioni, array e gli altri oggetti specializzati). Le
// operazioni sulle proprietà possono eseguire JS (getter, setter, trap dei Proxy) e quindi
// restituire ErrPendingException.
type Object struct{ Value }

// NewObject crea un oggetto vuoto ({}).
func (e Env) NewObject() (Object, error) {
	var r sys.Value
	st := sys.CreateObject(e.raw, &r)
	v, err := e.value(r, st)
	return Object{v}, err
}

// Get legge o[key].
func (o Object) Get(key string) (Value, error) {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return Value{}, err
	}
	var r sys.Value
	st := sys.GetProperty(o.env, o.raw, k, &r)
	return Env{raw: o.env}.value(r, st)
}

// Set scrive o[key] = v.
func (o Object) Set(key string, v Value) error {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return err
	}
	return check(o.env, sys.SetProperty(o.env, o.raw, k, v.raw))
}

// Has valuta key in o (prototipi compresi).
func (o Object) Has(key string) (bool, error) {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return false, err
	}
	var b bool
	st := sys.HasProperty(o.env, o.raw, k, &b)
	return b, check(o.env, st)
}

// HasOwn dice se o ha la proprietà propria key (Object.hasOwn).
func (o Object) HasOwn(key string) (bool, error) {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return false, err
	}
	var b bool
	st := sys.HasOwnProperty(o.env, o.raw, k, &b)
	return b, check(o.env, st)
}

// Delete valuta delete o[key]; restituisce false se la proprietà non è configurabile.
func (o Object) Delete(key string) (bool, error) {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return false, err
	}
	var b bool
	st := sys.DeleteProperty(o.env, o.raw, k, &b)
	return b, check(o.env, st)
}

// GetProperty legge o[key] con una chiave qualsiasi (string, symbol, number).
func (o Object) GetProperty(key Value) (Value, error) {
	var r sys.Value
	st := sys.GetProperty(o.env, o.raw, key.raw, &r)
	return Env{raw: o.env}.value(r, st)
}

// SetProperty scrive o[key] = v con una chiave qualsiasi.
func (o Object) SetProperty(key, v Value) error {
	return check(o.env, sys.SetProperty(o.env, o.raw, key.raw, v.raw))
}

// HasProperty valuta key in o con una chiave qualsiasi.
func (o Object) HasProperty(key Value) (bool, error) {
	var b bool
	st := sys.HasProperty(o.env, o.raw, key.raw, &b)
	return b, check(o.env, st)
}

// HasOwnProperty dice se o ha la proprietà propria key (string o symbol).
func (o Object) HasOwnProperty(key Value) (bool, error) {
	var b bool
	st := sys.HasOwnProperty(o.env, o.raw, key.raw, &b)
	return b, check(o.env, st)
}

// DeleteProperty valuta delete o[key] con una chiave qualsiasi.
func (o Object) DeleteProperty(key Value) (bool, error) {
	var b bool
	st := sys.DeleteProperty(o.env, o.raw, key.raw, &b)
	return b, check(o.env, st)
}

// GetIndex legge o[i].
func (o Object) GetIndex(i uint32) (Value, error) {
	var r sys.Value
	st := sys.GetElement(o.env, o.raw, i, &r)
	return Env{raw: o.env}.value(r, st)
}

// SetIndex scrive o[i] = v.
func (o Object) SetIndex(i uint32, v Value) error {
	return check(o.env, sys.SetElement(o.env, o.raw, i, v.raw))
}

// HasIndex valuta i in o.
func (o Object) HasIndex(i uint32) (bool, error) {
	var b bool
	st := sys.HasElement(o.env, o.raw, i, &b)
	return b, check(o.env, st)
}

// DeleteIndex valuta delete o[i].
func (o Object) DeleteIndex(i uint32) (bool, error) {
	var b bool
	st := sys.DeleteElement(o.env, o.raw, i, &b)
	return b, check(o.env, st)
}

// Keys restituisce le chiavi enumerabili di tipo stringa, prototipi compresi, come for...in
// (napi_get_property_names). Gli indici sono convertiti in stringhe.
func (o Object) Keys() (Array, error) {
	var r sys.Value
	st := sys.GetPropertyNames(o.env, o.raw, &r)
	v, err := Env{raw: o.env}.value(r, st)
	return Array{Object{v}}, err
}

// KeyCollectionMode, KeyFilter e KeyConversion scelgono le chiavi di AllKeys.
type (
	KeyCollectionMode = sys.KeyCollectionMode
	KeyFilter         = sys.KeyFilter
	KeyConversion     = sys.KeyConversion
)

const (
	KeyIncludePrototypes = sys.KeyIncludePrototypes
	KeyOwnOnly           = sys.KeyOwnOnly

	KeyAllProperties = sys.KeyAllProperties
	KeyWritable      = sys.KeyWritable
	KeyEnumerable    = sys.KeyEnumerable
	KeyConfigurable  = sys.KeyConfigurable
	KeySkipStrings   = sys.KeySkipStrings
	KeySkipSymbols   = sys.KeySkipSymbols

	KeyKeepNumbers      = sys.KeyKeepNumbers
	KeyNumbersToStrings = sys.KeyNumbersToStrings
)

// AllKeys restituisce le chiavi scelte da mode, filter (combinabile con |) e conversion
// (napi_get_all_property_names). Per le chiavi proprie enumerabili, come Object.keys:
// AllKeys(KeyOwnOnly, KeyEnumerable|KeySkipSymbols, KeyNumbersToStrings).
func (o Object) AllKeys(mode KeyCollectionMode, filter KeyFilter, conversion KeyConversion) (Array, error) {
	var r sys.Value
	st := sys.GetAllPropertyNames(o.env, o.raw, mode, filter, conversion, &r)
	v, err := Env{raw: o.env}.value(r, st)
	return Array{Object{v}}, err
}

// Prototype restituisce il prototipo (Object.getPrototypeOf).
func (o Object) Prototype() (Value, error) {
	var r sys.Value
	st := sys.GetPrototype(o.env, o.raw, &r)
	return Env{raw: o.env}.value(r, st)
}

// Freeze applica Object.freeze.
func (o Object) Freeze() error { return check(o.env, sys.ObjectFreeze(o.env, o.raw)) }

// Seal applica Object.seal.
func (o Object) Seal() error { return check(o.env, sys.ObjectSeal(o.env, o.raw)) }

// ---- array ----

// Array è un array JS.
type Array struct{ Object }

// NewArray crea un array vuoto ([]).
func (e Env) NewArray() (Array, error) {
	var r sys.Value
	st := sys.CreateArray(e.raw, &r)
	v, err := e.value(r, st)
	return Array{Object{v}}, err
}

// NewArrayWithLength crea un array di length elementi vuoti (new Array(length)).
func (e Env) NewArrayWithLength(length int) (Array, error) {
	if length < 0 {
		return Array{}, RangeError("", "negative array length")
	}
	var r sys.Value
	st := sys.CreateArrayWithLength(e.raw, uint(length), &r)
	v, err := e.value(r, st)
	return Array{Object{v}}, err
}

// Len restituisce la proprietà length.
func (a Array) Len() (uint32, error) {
	var n uint32
	st := sys.GetArrayLength(a.env, a.raw, &n)
	return n, check(a.env, st)
}

// ---- definizione di proprietà ----

// PropertyAttributes sono gli attributi di una proprietà (napi_property_attributes), combinabili
// con |.
type PropertyAttributes = sys.PropertyAttributes

const (
	// Default: non scrivibile, non enumerabile, non configurabile.
	Default      = sys.PropertyDefault
	Writable     = sys.PropertyWritable
	Enumerable   = sys.PropertyEnumerable
	Configurable = sys.PropertyConfigurable
	// Static: proprietà del costruttore invece che del prototipo (solo DefineClass).
	Static = sys.PropertyStatic
	// DefaultMethod: Writable|Configurable, come i metodi delle classi JS.
	DefaultMethod = sys.PropertyDefaultMethod
	// DefaultJSProperty: Writable|Enumerable|Configurable, come un'assegnazione JS.
	DefaultJSProperty = sys.PropertyDefaultJSProperty
)

// Property descrive una proprietà per DefineProperties e DefineClass. Va impostato uno solo tra
// Method, Getter/Setter e Value. Name è la chiave; per una chiave symbol si usa Key.
type Property struct {
	Name       string
	Key        Value
	Method     Callback
	Getter     Callback
	Setter     Callback
	Value      Value
	Attributes PropertyAttributes
}

// DefineProperties definisce le proprietà su o (napi_define_properties). Le callback restano
// registrate finché o vive.
func (o Object) DefineProperties(props ...Property) error {
	if len(props) == 0 {
		return nil
	}
	env := Env{raw: o.env}
	descs, ids, err := env.descriptors(props)
	if err != nil {
		return err
	}
	if err := check(o.env, sys.DefineProperties(o.env, o.raw, uint(len(descs)), &descs[0])); err != nil {
		releaseCallbacks(ids)
		return err
	}
	return env.releaseCallbacksWith(o, ids)
}

// descriptors converte props nei descrittori di Node-API registrando le callback. In caso di
// errore le callback già registrate vengono rilasciate.
func (e Env) descriptors(props []Property) ([]sys.PropertyDescriptor, []uintptr, error) {
	descs := make([]sys.PropertyDescriptor, len(props))
	var ids []uintptr
	fail := func(err error) ([]sys.PropertyDescriptor, []uintptr, error) {
		releaseCallbacks(ids)
		return nil, nil, err
	}
	for i := range props {
		p := &props[i]
		d := &descs[i]
		if p.Key.raw != nil {
			d.Name = p.Key.raw
		} else {
			k, err := e.propertyKey(p.Name)
			if err != nil {
				return fail(err)
			}
			d.Name = k
		}
		d.Attributes = p.Attributes
		switch {
		case p.Method != nil:
			if p.Getter != nil || p.Setter != nil || p.Value.raw != nil {
				return fail(TypeError("", "property "+p.Name+": Method excludes Getter, Setter and Value"))
			}
			id := registerCallback(p.Method)
			ids = append(ids, id)
			d.Method, d.Data = sys.CallbackTrampoline, id
		case p.Getter != nil || p.Setter != nil:
			if p.Value.raw != nil {
				return fail(TypeError("", "property "+p.Name+": an accessor excludes Value"))
			}
			var id uintptr
			if p.Setter == nil {
				id = registerCallback(p.Getter)
				d.Getter = sys.CallbackTrampoline
			} else {
				// Getter e setter condividono il data del descrittore: un'unica callback li
				// distingue dal numero di argomenti (vedi accessor).
				id = registerCallback(accessor(p.Getter, p.Setter))
				d.Setter = sys.CallbackTrampoline
				if p.Getter != nil {
					d.Getter = sys.CallbackTrampoline
				}
			}
			ids = append(ids, id)
			d.Data = id
		default:
			if p.Value.raw == nil {
				return fail(TypeError("", "property "+p.Name+": one of Method, Getter, Setter or Value is required"))
			}
			d.Value = p.Value.raw
		}
	}
	return descs, ids, nil
}
