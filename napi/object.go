package napi

import (
	"github.com/napi-bindings/napi-go/sys"
)

// Object represents a JS object (including functions, arrays, and other specialized objects).
// Property operations may execute JS (getters, setters, Proxy traps) and therefore may return
// ErrPendingException.
type Object struct{ Value }

// NewObject creates a new empty object ({}).
func (e Env) NewObject() (Object, error) {
	var r sys.Value
	st := sys.CreateObject(e.raw, &r)
	v, err := e.value(r, st)
	return Object{v}, err
}

// Get reads o[key].
func (o Object) Get(key string) (Value, error) {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return Value{}, err
	}
	var r sys.Value
	st := sys.GetProperty(o.env, o.raw, k, &r)
	return Env{raw: o.env}.value(r, st)
}

// Set writes o[key] = v.
func (o Object) Set(key string, v Value) error {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return err
	}
	return check(o.env, sys.SetProperty(o.env, o.raw, k, v.raw))
}

// Has evaluates key in o (including the prototype chain).
func (o Object) Has(key string) (bool, error) {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return false, err
	}
	var b bool
	st := sys.HasProperty(o.env, o.raw, k, &b)
	return b, check(o.env, st)
}

// HasOwn reports whether o has the own property key (Object.hasOwn).
func (o Object) HasOwn(key string) (bool, error) {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return false, err
	}
	var b bool
	st := sys.HasOwnProperty(o.env, o.raw, k, &b)
	return b, check(o.env, st)
}

// Delete evaluates delete o[key]; returns false if the property is non-configurable.
func (o Object) Delete(key string) (bool, error) {
	k, err := Env{raw: o.env}.propertyKey(key)
	if err != nil {
		return false, err
	}
	var b bool
	st := sys.DeleteProperty(o.env, o.raw, k, &b)
	return b, check(o.env, st)
}

// GetProperty reads o[key] using an arbitrary key Value (string, symbol, or number).
func (o Object) GetProperty(key Value) (Value, error) {
	var r sys.Value
	st := sys.GetProperty(o.env, o.raw, key.raw, &r)
	return Env{raw: o.env}.value(r, st)
}

// SetProperty writes o[key] = v using an arbitrary key Value.
func (o Object) SetProperty(key, v Value) error {
	return check(o.env, sys.SetProperty(o.env, o.raw, key.raw, v.raw))
}

// HasProperty evaluates key in o using an arbitrary key Value.
func (o Object) HasProperty(key Value) (bool, error) {
	var b bool
	st := sys.HasProperty(o.env, o.raw, key.raw, &b)
	return b, check(o.env, st)
}

// HasOwnProperty reports whether o has the own property key (string or symbol).
func (o Object) HasOwnProperty(key Value) (bool, error) {
	var b bool
	st := sys.HasOwnProperty(o.env, o.raw, key.raw, &b)
	return b, check(o.env, st)
}

// DeleteProperty evaluates delete o[key] using an arbitrary key Value.
func (o Object) DeleteProperty(key Value) (bool, error) {
	var b bool
	st := sys.DeleteProperty(o.env, o.raw, key.raw, &b)
	return b, check(o.env, st)
}

// GetIndex reads o[i].
func (o Object) GetIndex(i uint32) (Value, error) {
	var r sys.Value
	st := sys.GetElement(o.env, o.raw, i, &r)
	return Env{raw: o.env}.value(r, st)
}

// SetIndex writes o[i] = v.
func (o Object) SetIndex(i uint32, v Value) error {
	return check(o.env, sys.SetElement(o.env, o.raw, i, v.raw))
}

// HasIndex evaluates i in o.
func (o Object) HasIndex(i uint32) (bool, error) {
	var b bool
	st := sys.HasElement(o.env, o.raw, i, &b)
	return b, check(o.env, st)
}

// DeleteIndex evaluates delete o[i].
func (o Object) DeleteIndex(i uint32) (bool, error) {
	var b bool
	st := sys.DeleteElement(o.env, o.raw, i, &b)
	return b, check(o.env, st)
}

// Keys returns the enumerable string keys of o, including prototypes, like for...in
// (napi_get_property_names). Array indices are converted to strings.
func (o Object) Keys() (Array, error) {
	var r sys.Value
	st := sys.GetPropertyNames(o.env, o.raw, &r)
	v, err := Env{raw: o.env}.value(r, st)
	return Array{Object{v}}, err
}

// KeyCollectionMode, KeyFilter, and KeyConversion select which keys are returned by AllKeys.
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

// AllKeys returns the property keys selected by mode, filter (combinable with |), and conversion
// (napi_get_all_property_names). For own enumerable string keys (equivalent to Object.keys):
// AllKeys(KeyOwnOnly, KeyEnumerable|KeySkipSymbols, KeyNumbersToStrings).
func (o Object) AllKeys(mode KeyCollectionMode, filter KeyFilter, conversion KeyConversion) (Array, error) {
	var r sys.Value
	st := sys.GetAllPropertyNames(o.env, o.raw, mode, filter, conversion, &r)
	v, err := Env{raw: o.env}.value(r, st)
	return Array{Object{v}}, err
}

// Prototype returns the prototype of o (Object.getPrototypeOf).
func (o Object) Prototype() (Value, error) {
	var r sys.Value
	st := sys.GetPrototype(o.env, o.raw, &r)
	return Env{raw: o.env}.value(r, st)
}

// Freeze applies Object.freeze to o.
func (o Object) Freeze() error { return check(o.env, sys.ObjectFreeze(o.env, o.raw)) }

// Seal applies Object.seal to o.
func (o Object) Seal() error { return check(o.env, sys.ObjectSeal(o.env, o.raw)) }

// ---- array ----

// Array represents a JS array.
type Array struct{ Object }

// NewArray creates a new empty array ([]).
func (e Env) NewArray() (Array, error) {
	var r sys.Value
	st := sys.CreateArray(e.raw, &r)
	v, err := e.value(r, st)
	return Array{Object{v}}, err
}

// NewArrayWithLength creates an array of length empty slots (new Array(length)).
func (e Env) NewArrayWithLength(length int) (Array, error) {
	if length < 0 {
		return Array{}, RangeError("", "negative array length")
	}
	var r sys.Value
	st := sys.CreateArrayWithLength(e.raw, uint(length), &r)
	v, err := e.value(r, st)
	return Array{Object{v}}, err
}

// Len returns the array's length property.
func (a Array) Len() (uint32, error) {
	var n uint32
	st := sys.GetArrayLength(a.env, a.raw, &n)
	return n, check(a.env, st)
}

// ---- property definition ----

// PropertyAttributes represents property descriptor flags (napi_property_attributes), combinable
// with |.
type PropertyAttributes = sys.PropertyAttributes

const (
	// Default specifies a non-writable, non-enumerable, non-configurable property.
	Default      = sys.PropertyDefault
	Writable     = sys.PropertyWritable
	Enumerable   = sys.PropertyEnumerable
	Configurable = sys.PropertyConfigurable
	// Static defines the property on the constructor rather than the prototype (DefineClass only).
	Static = sys.PropertyStatic
	// DefaultMethod is Writable|Configurable, matching standard JS class methods.
	DefaultMethod = sys.PropertyDefaultMethod
	// DefaultJSProperty is Writable|Enumerable|Configurable, matching standard JS property assignment.
	DefaultJSProperty = sys.PropertyDefaultJSProperty
)

// Property describes a property for DefineProperties and DefineClass. Exactly one of Method,
// Getter/Setter, or Value must be set. Name is the string key; use Key for a symbol key.
type Property struct {
	Name       string
	Key        Value
	Method     Callback
	Getter     Callback
	Setter     Callback
	Value      Value
	Attributes PropertyAttributes
}

// DefineProperties defines properties on o (napi_define_properties). Any callbacks remain
// registered for as long as o is alive.
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

// descriptors converts props into Node-API property descriptors and registers their callbacks. If
// an error occurs, any already-registered callbacks are released.
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
				// Getter and setter share the descriptor's data field: a single callback
				// distinguishes between them by argument count (see accessor).
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
