package main

// Port of test/object/*.cc and test/globalObject/*.cc.
//
// The C++ tests reach each Object operation with every key type node-addon-api overloads: a
// uint32 index, a napi_value, a Napi::Name, a C string and a std::string. In Go the last two are
// the same string-keyed method, and the napi_value and Napi::Name forms are the same Value-keyed
// method. info[0].UnsafeAs<Object>() is napi.Object{Value: v}, without a type check: Node-API
// then fails on undefined with V8's "Cannot convert undefined or null to object".
//
// Not ported: Object iteration (sum, increment; they require C++ exceptions and the scripts skip
// them when absent), setPrototype and createObjectWithProperties (experimental Node-API, skipped
// by the script when absent).

import (
	"github.com/napi-bindings/napi-go/napi"
)

// keyKind selects how an exported function passes the key (its second argument) to the Object
// method under test.
type keyKind int

const (
	keyIndex  keyKind = iota // info[1].As<Number>().Uint32Value()
	keyValue                 // napi_value / Napi::Name
	keyString                // C string / std::string
)

// objectKey is the key of a property access, decoded from a JS argument according to its kind.
type objectKey struct {
	kind  keyKind
	index uint32
	value napi.Value
	name  string
}

func readKey(v napi.Value, kind keyKind) (objectKey, error) {
	k := objectKey{kind: kind, value: v}
	var err error
	switch kind {
	case keyIndex:
		k.index, err = v.Uint32()
	case keyString:
		k.name, err = v.GoString()
	}
	return k, err
}

func getKey(o napi.Object, k objectKey) (napi.Value, error) {
	switch k.kind {
	case keyIndex:
		return o.GetIndex(k.index)
	case keyValue:
		return o.GetProperty(k.value)
	}
	return o.Get(k.name)
}

func setKey(o napi.Object, k objectKey, v napi.Value) error {
	switch k.kind {
	case keyIndex:
		return o.SetIndex(k.index, v)
	case keyValue:
		return o.SetProperty(k.value, v)
	}
	return o.Set(k.name, v)
}

func hasKey(o napi.Object, k objectKey) (bool, error) {
	switch k.kind {
	case keyIndex:
		return o.HasIndex(k.index)
	case keyValue:
		return o.HasProperty(k.value)
	}
	return o.Has(k.name)
}

func hasOwnKey(o napi.Object, k objectKey) (bool, error) {
	switch k.kind {
	case keyIndex:
		// Object::HasOwnProperty has no uint32 overload: an index is a string-keyed property.
		key, err := o.Env().NewUint32(k.index)
		if err != nil {
			return false, err
		}
		return o.HasOwnProperty(key)
	case keyValue:
		return o.HasOwnProperty(k.value)
	}
	return o.HasOwn(k.name)
}

func deleteKey(o napi.Object, k objectKey) (bool, error) {
	switch k.kind {
	case keyIndex:
		return o.DeleteIndex(k.index)
	case keyValue:
		return o.DeleteProperty(k.value)
	}
	return o.Delete(k.name)
}

// unsafeObject is info[i].UnsafeAs<Object>(): the value is used as an object without checking.
func unsafeObject(v napi.Value) napi.Object { return napi.Object{Value: v} }

// objectOp returns an export operating on (object, key[, value]) arguments, with the object read
// by target and the key read from argument keyArg.
func objectOp(target func(c napi.CallContext) napi.Object, keyArg int, kind keyKind,
	op func(c napi.CallContext, o napi.Object, k objectKey) (napi.Value, error)) napi.Callback {
	return func(c napi.CallContext) (napi.Value, error) {
		k, err := readKey(c.Arg(keyArg), kind)
		if err != nil {
			return napi.Value{}, err
		}
		return op(c, target(c), k)
	}
}

func argTarget(c napi.CallContext) napi.Object    { return unsafeObject(c.Arg(0)) }
func globalTarget(c napi.CallContext) napi.Object { return c.Env().Global() }

func opGet(_ napi.CallContext, o napi.Object, k objectKey) (napi.Value, error) { return getKey(o, k) }

// opSetReturningTrue sets the property to the argument after the key and returns true, like Object::Set
// returning Maybe<bool>.
func opSetReturningTrue(valueArg int) func(c napi.CallContext, o napi.Object, k objectKey) (napi.Value, error) {
	return func(c napi.CallContext, o napi.Object, k objectKey) (napi.Value, error) {
		if err := setKey(o, k, c.Arg(valueArg)); err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewBoolean(true)
	}
}

func opSetVoid(valueArg int) func(c napi.CallContext, o napi.Object, k objectKey) (napi.Value, error) {
	return func(c napi.CallContext, o napi.Object, k objectKey) (napi.Value, error) {
		return napi.Value{}, setKey(o, k, c.Arg(valueArg))
	}
}

func boolOp(f func(o napi.Object, k objectKey) (bool, error)) func(c napi.CallContext, o napi.Object, k objectKey) (napi.Value, error) {
	return func(c napi.CallContext, o napi.Object, k objectKey) (napi.Value, error) {
		b, err := f(o, k)
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewBoolean(b)
	}
}

// userDataHolder is the per-defineProperties data shared by the "WithUserData" accessors.
type userDataHolder struct{ value int32 }

// objectTestValue is the value behind the readonly/readwrite accessors (testValue in object.cc).
var objectTestValue = true

// magicIndex and magicIndexValue are the obj[(uint32_t)42] = 120 entry of createObjectUsingMagic.
const (
	magicIndex      = 42
	magicIndexValue = 120
	// holderInitialValue is UserDataHolder::value as set by defineProperties.
	holderInitialValue = 1234
)

func initObject(env napi.Env) (napi.Object, error) {
	fns := []fn{
		{"emptyConstructor", func(c napi.CallContext) (napi.Value, error) {
			isEmpty, err := argBool(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			o := napi.Object{}
			if !isEmpty {
				if o, err = c.Env().NewObject(); err != nil {
					return napi.Value{}, err
				}
			}
			return c.Env().NewBoolean(o.IsEmpty())
		}},
		{"constructorFromObject", func(c napi.CallContext) (napi.Value, error) {
			o, err := argObject(c, 0)
			return o.Value, err
		}},
		{"GetPropertyNames", func(c napi.CallContext) (napi.Value, error) {
			o, err := argObject(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			keys, err := o.Keys()
			return keys.Value, err
		}},
		{"defineProperties", defineProperties},
		{"defineValueProperty", func(c napi.CallContext) (napi.Value, error) {
			o, err := argObject(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			return napi.Value{}, o.DefineProperties(napi.Property{Key: c.Arg(1), Value: c.Arg(2)})
		}},
		{"createObjectUsingMagic", createObjectUsingMagic},
		{"addFinalizer", addFinalizer("finalizerCalled")},
		{"addFinalizerWithHint", addFinalizer("finalizerCalledWithCorrectHint")},
		{"instanceOf", func(c napi.CallContext) (napi.Value, error) {
			ctor, err := argFunction(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			ok, err := c.Arg(0).InstanceOf(ctor)
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(ok)
		}},
		{"getPrototype", func(c napi.CallContext) (napi.Value, error) { return unsafeObject(c.Arg(0)).Prototype() }},
		// Object::operator[] reads and writes with the same overloads as Get and Set.
		{"subscriptGetWithCStyleString", objectOp(argTarget, 1, keyString, opGet)},
		{"subscriptGetWithCppStyleString", objectOp(argTarget, 1, keyString, opGet)},
		{"subscriptGetAtIndex", objectOp(argTarget, 1, keyIndex, opGet)},
		{"subscriptSetWithCStyleString", objectOp(argTarget, 1, keyString, opSetVoid(2))},
		{"subscriptSetWithCppStyleString", objectOp(argTarget, 1, keyString, opSetVoid(2))},
		{"subscriptSetAtIndex", objectOp(argTarget, 1, keyIndex, opSetVoid(2))},
	}
	// The five overload families of object/{get,set,delete,has_own,has}_property.cc.
	variants := []struct {
		suffix string
		kind   keyKind
	}{
		{"WithUint32", keyIndex},
		{"WithNapiValue", keyValue},
		{"WithNapiWrapperValue", keyValue},
		{"WithCStyleString", keyString},
		{"WithCppStyleString", keyString},
	}
	for _, v := range variants {
		fns = append(fns,
			fn{"getProperty" + v.suffix, objectOp(argTarget, 1, v.kind, opGet)},
			fn{"setProperty" + v.suffix, objectOp(argTarget, 1, v.kind, opSetReturningTrue(2))},
			fn{"deleteProperty" + v.suffix, objectOp(argTarget, 1, v.kind, boolOp(deleteKey))},
			fn{"hasProperty" + v.suffix, objectOp(argTarget, 1, v.kind, boolOp(hasKey))},
		)
		if v.kind != keyIndex {
			fns = append(fns, fn{"hasOwnProperty" + v.suffix, objectOp(argTarget, 1, v.kind, boolOp(hasOwnKey))})
		}
	}
	return newExports(env, fns...)
}

func defineProperties(c napi.CallContext) (napi.Value, error) {
	o, err := argObject(c, 0)
	if err != nil {
		return napi.Value{}, err
	}
	nameType, err := argString(c, 1)
	if err != nil {
		return napi.Value{}, err
	}
	env := c.Env()
	trueValue, err := env.NewBoolean(true)
	if err != nil {
		return napi.Value{}, err
	}
	holder := &userDataHolder{value: holderInitialValue}
	getter := func(c napi.CallContext) (napi.Value, error) { return c.Env().NewBoolean(objectTestValue) }
	setter := func(c napi.CallContext) (napi.Value, error) {
		b, err := argBool(c, 0)
		objectTestValue = b
		return napi.Value{}, err
	}
	getterWithUserData := func(c napi.CallContext) (napi.Value, error) {
		return c.Env().NewInt32(holder.value)
	}
	setterWithUserData := func(c napi.CallContext) (napi.Value, error) {
		i, err := c.Arg(0).Int32()
		holder.value = i
		return napi.Value{}, err
	}
	function := func(c napi.CallContext) (napi.Value, error) { return c.Env().NewBoolean(true) }
	props := []napi.Property{
		{Name: "readonlyAccessor", Getter: getter},
		{Name: "readwriteAccessor", Getter: getter, Setter: setter},
		{Name: "readonlyAccessorWithUserData", Getter: getterWithUserData},
		{Name: "readwriteAccessorWithUserData", Getter: getterWithUserData, Setter: setterWithUserData},
		// The templated C++ descriptors (Accessor<Getter>) are the same descriptors.
		{Name: "readonlyAccessorT", Getter: getter},
		{Name: "readwriteAccessorT", Getter: getter, Setter: setter},
		{Name: "readonlyAccessorWithUserDataT", Getter: getterWithUserData},
		{Name: "readwriteAccessorWithUserDataT", Getter: getterWithUserData, Setter: setterWithUserData},
		{Name: "readonlyValue", Value: trueValue},
		{Name: "readwriteValue", Value: trueValue, Attributes: napi.Writable},
		{Name: "enumerableValue", Value: trueValue, Attributes: napi.Enumerable},
		{Name: "configurableValue", Value: trueValue, Attributes: napi.Configurable},
		{Name: "function", Method: function},
		{Name: "functionWithUserData", Method: getterWithUserData},
	}
	switch nameType {
	case "literal", "string":
		// C strings and std::string are both Go strings.
	case "value":
		// Keys passed as Napi::String values.
		for i := range props {
			k, err := env.NewString(props[i].Name)
			if err != nil {
				return napi.Value{}, err
			}
			props[i].Key = k.Value
		}
	default:
		return napi.Value{}, nil
	}
	return napi.Value{}, o.DefineProperties(props...)
}

func createObjectUsingMagic(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	o, err := env.NewObject()
	if err != nil {
		return napi.Value{}, err
	}
	f, t := must(env.NewBoolean(false)), must(env.NewBoolean(true))
	zero := must(env.NewFloat64(0))
	foo := must(str(env, "foo"))
	props := map[string]napi.Value{
		"cp_false": f, "cp_true": t, "s_true": t, "s_false": f,
		"0": zero, "0.0f": zero, "0.0": zero, "-1": must(env.NewInt32(-1)),
		"foo2": foo, "foo4": foo, "foo5": foo, "foo6": foo, "foo7": foo,
		"circular": o.Value, "circular2": o.Value,
	}
	if err := setProps(o, props); err != nil {
		return napi.Value{}, err
	}
	return o.Value, o.SetIndex(magicIndex, must(env.NewInt32(magicIndexValue)))
}

// addFinalizer returns an export that attaches a finalizer to its argument and returns a new
// object, kept alive by a reference, on which the finalizer sets property to true.
func addFinalizer(property string) napi.Callback {
	return func(c napi.CallContext) (napi.Value, error) {
		target, err := argObject(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		result, err := c.Env().NewObject()
		if err != nil {
			return napi.Value{}, err
		}
		ref, err := result.NewRef(1)
		if err != nil {
			return napi.Value{}, err
		}
		err = target.AddFinalizer(func(env napi.Env) {
			defer ref.Delete()
			v, err := ref.Value()
			if err != nil {
				return
			}
			t, err := env.NewBoolean(true)
			if err != nil {
				return
			}
			unsafeObject(v).Set(property, t)
		})
		return result.Value, err
	}
}

func initObjectFreezeSeal(env napi.Env) (napi.Object, error) {
	op := func(f func(napi.Object) error) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			o, err := argObject(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			if err := f(o); err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(true)
		}
	}
	return newExports(env,
		fn{"freeze", op(napi.Object.Freeze)},
		fn{"seal", op(napi.Object.Seal)},
	)
}

// Global object mock values set by createMockTestObject.
const (
	mockNumberKey = 2
	mockIndexKey  = 15
)

func initGlobalObject(env napi.Env) (napi.Object, error) {
	return newExports(env,
		fn{"getPropertyWithInt32", objectOp(globalTarget, 0, keyIndex, opGet)},
		fn{"getPropertyWithNapiValue", objectOp(globalTarget, 0, keyValue, opGet)},
		fn{"getPropertyWithCppString", objectOp(globalTarget, 0, keyString, opGet)},
		fn{"getPropertyWithCString", objectOp(globalTarget, 0, keyString, opGet)},
		fn{"createMockTestObject", func(c napi.CallContext) (napi.Value, error) {
			env := c.Env()
			g := env.Global()
			if err := g.SetProperty(must(env.NewInt32(mockNumberKey)), must(str(env, "napi_attribute"))); err != nil {
				return napi.Value{}, err
			}
			props := map[string]napi.Value{
				"c_str_key":      must(str(env, "c_string_attribute")),
				"cpp_string_key": must(str(env, "cpp_string_attribute")),
				"circular":       g.Value,
			}
			if err := setProps(g, props); err != nil {
				return napi.Value{}, err
			}
			return napi.Value{}, g.SetIndex(mockIndexKey, must(env.NewInt32(mockIndexKey)))
		}},
		fn{"setPropertyWithCStyleString", objectOp(globalTarget, 0, keyString, opSetVoid(1))},
		fn{"setPropertyWithCppStyleString", objectOp(globalTarget, 0, keyString, opSetVoid(1))},
		fn{"setPropertyWithNapiValue", objectOp(globalTarget, 0, keyValue, opSetVoid(1))},
		fn{"setPropertyWithInt32", objectOp(globalTarget, 0, keyIndex, opSetVoid(1))},
		fn{"hasPropertyWithCStyleString", objectOp(globalTarget, 0, keyString, boolOp(hasOwnKey))},
		fn{"hasPropertyWithCppStyleString", objectOp(globalTarget, 0, keyString, boolOp(hasOwnKey))},
		fn{"hasPropertyWithNapiValue", objectOp(globalTarget, 0, keyValue, boolOp(hasOwnKey))},
		fn{"deletePropertyWithCStyleString", objectOp(globalTarget, 0, keyString, boolOp(deleteKey))},
		fn{"deletePropertyWithCppStyleString", objectOp(globalTarget, 0, keyString, boolOp(deleteKey))},
		fn{"deletePropertyWithInt32", objectOp(globalTarget, 0, keyIndex, boolOp(deleteKey))},
		fn{"deletePropertyWithNapiValue", objectOp(globalTarget, 0, keyValue, boolOp(deleteKey))},
	)
}
