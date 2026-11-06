package main

// Port of test/objectwrap.cc, test/objectwrap_removewrap.cc and
// test/objectwrap_constructor_exception.cc.
//
// Napi::ObjectWrap<T> is a class whose constructor wraps a new C++ T in `this` and whose instance
// callbacks unwrap it. With napi: DefineClass, a constructor calling Wrap on c.This(), and
// callbacks calling UnwrapAs. The templated accessors and methods (InstanceAccessor<&T::Getter>)
// are the same properties as the non-templated ones.
//
// When unwrapping fails (an instance callback invoked on the prototype), napi returns
// ErrNotWrapped, whose message differs from Node-API's "Invalid argument": the four assertions of
// test/objectwrap.js that match the message accept both.

import (
	"github.com/napi-bindings/napi-go/napi"
)

// wrapTest is the native counterpart of a JS Test instance (class Test in objectwrap.cc).
type wrapTest struct {
	value      string
	finalizeCb napi.Ref // empty when the constructor received no callback
	bufRef     napi.Ref
}

// staticMethodText is Test::s_staticMethodText.
var staticMethodText string

func unwrapTest(c napi.CallContext) (*wrapTest, error) { return napi.UnwrapAs[*wrapTest](c.This()) }

// suffixMethod returns a callback returning ToString(arg0) + suffix, on an instance when instance
// is true (C++ instance methods unwrap this first).
func suffixMethod(suffix string, instance bool) napi.Callback {
	return func(c napi.CallContext) (napi.Value, error) {
		if instance {
			if _, err := unwrapTest(c); err != nil {
				return napi.Value{}, err
			}
		}
		s, err := toGoString(c.Arg(0))
		if err != nil {
			return napi.Value{}, err
		}
		return str(c.Env(), s+suffix)
	}
}

func initObjectWrap(env napi.Env) (napi.Object, error) {
	// testStaticContextRef: a persistent object holding the value behind the static accessors.
	staticContext, err := env.NewObject()
	if err != nil {
		return napi.Object{}, err
	}
	staticContextRef, err := staticContext.NewRef(1)
	if err != nil {
		return napi.Object{}, err
	}
	staticGetter := func(c napi.CallContext) (napi.Value, error) {
		ctx, err := staticContextRef.Value()
		if err != nil {
			return napi.Value{}, err
		}
		return unsafeObject(ctx).Get("value")
	}
	staticSetter := func(c napi.CallContext) (napi.Value, error) {
		ctx, err := staticContextRef.Value()
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, unsafeObject(ctx).Set("value", c.Arg(0))
	}
	staticVoidMethod := func(c napi.CallContext) (napi.Value, error) {
		if _, err := c.Arg(0).Float64(); err != nil {
			return napi.Value{}, err
		}
		return staticSetter(c)
	}
	staticMethodT := func(c napi.CallContext) (napi.Value, error) { return str(c.Env(), staticMethodText) }
	staticVoidMethodT := func(c napi.CallContext) (napi.Value, error) {
		s, err := toGoString(c.Arg(0))
		staticMethodText = s
		return napi.Value{}, err
	}
	getter := func(c napi.CallContext) (napi.Value, error) {
		t, err := unwrapTest(c)
		if err != nil {
			return napi.Value{}, err
		}
		return str(c.Env(), t.value)
	}
	setter := func(c napi.CallContext) (napi.Value, error) {
		t, err := unwrapTest(c)
		if err != nil {
			return napi.Value{}, err
		}
		s, err := toGoString(c.Arg(0))
		t.value = s
		return napi.Value{}, err
	}
	canUnWrap := func(c napi.CallContext) (napi.Value, error) {
		o, err := argObject(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		want, err := argString(c, 1)
		if err != nil {
			return napi.Value{}, err
		}
		t, err := napi.UnwrapAs[*wrapTest](o)
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewBoolean(t.value == want)
	}
	toStringTag := func(c napi.CallContext) (napi.Value, error) { return str(c.Env(), "TestTag") }
	iterator := func(c napi.CallContext) (napi.Value, error) {
		// ([value])[Symbol.iterator]()
		t, err := unwrapTest(c)
		if err != nil {
			return napi.Value{}, err
		}
		env := c.Env()
		a, err := env.NewArray()
		if err != nil {
			return napi.Value{}, err
		}
		if err := a.SetIndex(0, must(str(env, t.value))); err != nil {
			return napi.Value{}, err
		}
		sym, err := wellKnownSymbol(env, "iterator")
		if err != nil {
			return napi.Value{}, err
		}
		f, err := a.GetProperty(sym)
		if err != nil {
			return napi.Value{}, err
		}
		iter, err := f.AsFunction()
		if err != nil {
			return napi.Value{}, err
		}
		return iter.Call(a.Value)
	}
	methodT := func(c napi.CallContext) (napi.Value, error) { return getter(c) }
	voidMethodT := func(c napi.CallContext) (napi.Value, error) { return setter(c) }

	// The symbols exposed as static values for the test, in objectwrap.cc order.
	symbolNames := []string{
		"kTestStaticValueInternal", "kTestStaticAccessorInternal", "kTestStaticAccessorTInternal",
		"kTestStaticMethodInternal", "kTestStaticMethodTInternal", "kTestStaticVoidMethodInternal",
		"kTestStaticVoidMethodTInternal", "kTestValueInternal", "kTestAccessorInternal",
		"kTestAccessorTInternal", "kTestMethodInternal", "kTestMethodTInternal", "kTestVoidMethodTInternal",
	}
	sym := map[string]napi.Value{}
	var props []napi.Property
	for _, name := range symbolNames {
		s, err := env.NewSymbol(name)
		if err != nil {
			return napi.Object{}, err
		}
		sym[name] = s.Value
		props = append(props, napi.Property{Name: name, Value: s.Value, Attributes: napi.Static})
	}
	toStringTagSymbol, err := wellKnownSymbol(env, "toStringTag")
	if err != nil {
		return napi.Object{}, err
	}
	iteratorSymbol, err := wellKnownSymbol(env, "iterator")
	if err != nil {
		return napi.Object{}, err
	}
	const static, enumerable = napi.Static, napi.Enumerable
	props = append(props,
		napi.Property{Name: "testStaticValue", Value: must(str(env, "value")), Attributes: static | enumerable},
		napi.Property{Key: sym["kTestStaticValueInternal"], Value: must(env.NewInt32(5)), Attributes: static},
		napi.Property{Name: "testStaticGetter", Getter: staticGetter, Attributes: static | enumerable},
		napi.Property{Name: "testStaticSetter", Setter: staticSetter, Attributes: static},
		napi.Property{Name: "testStaticGetSet", Getter: staticGetter, Setter: staticSetter, Attributes: static | enumerable},
		napi.Property{Key: sym["kTestStaticAccessorInternal"], Getter: staticGetter, Setter: staticSetter, Attributes: static | enumerable},
		napi.Property{Name: "testStaticGetterT", Getter: staticGetter, Attributes: static},
		napi.Property{Name: "testStaticGetSetT", Getter: staticGetter, Setter: staticSetter, Attributes: static},
		napi.Property{Key: sym["kTestStaticAccessorTInternal"], Getter: staticGetter, Setter: staticSetter, Attributes: static},
		napi.Property{Name: "testStaticVoidMethod", Method: staticVoidMethod, Attributes: static},
		napi.Property{Key: sym["kTestStaticVoidMethodInternal"], Method: staticVoidMethod, Attributes: static},
		napi.Property{Name: "testStaticMethod", Method: suffixMethod(" static", false), Attributes: static | enumerable},
		napi.Property{Key: sym["kTestStaticMethodInternal"], Method: suffixMethod(" static internal", false), Attributes: static},
		napi.Property{Name: "testStaticVoidMethodT", Method: staticVoidMethodT, Attributes: static},
		napi.Property{Name: "testStaticMethodT", Method: staticMethodT, Attributes: static},
		napi.Property{Key: sym["kTestStaticVoidMethodTInternal"], Method: staticVoidMethodT, Attributes: static},
		napi.Property{Key: sym["kTestStaticMethodTInternal"], Method: staticMethodT, Attributes: static},
		napi.Property{Name: "canUnWrap", Method: canUnWrap, Attributes: static | enumerable},
		napi.Property{Name: "testValue", Value: must(env.NewBoolean(true)), Attributes: enumerable},
		napi.Property{Key: sym["kTestValueInternal"], Value: must(env.NewBoolean(false)), Attributes: enumerable},
		napi.Property{Name: "testGetter", Getter: getter, Attributes: enumerable},
		napi.Property{Name: "testSetter", Setter: setter},
		napi.Property{Name: "testGetSet", Getter: getter, Setter: setter, Attributes: enumerable},
		napi.Property{Key: sym["kTestAccessorInternal"], Getter: getter, Setter: setter, Attributes: enumerable},
		napi.Property{Name: "testGetterT", Getter: getter},
		napi.Property{Name: "testGetSetT", Getter: getter, Setter: setter},
		// objectwrap.cc defines kTestAccessorInternal a second time where kTestAccessorTInternal
		// is meant; the script uses kTestAccessorTInternal as a plain property afterwards.
		napi.Property{Key: sym["kTestAccessorInternal"], Getter: getter, Setter: setter},
		napi.Property{Name: "testMethod", Method: suffixMethod(" instance", true), Attributes: enumerable},
		napi.Property{Key: sym["kTestMethodInternal"], Method: suffixMethod(" instance internal", true)},
		napi.Property{Name: "testMethodT", Method: methodT},
		napi.Property{Name: "testVoidMethodT", Method: voidMethodT},
		napi.Property{Key: sym["kTestMethodTInternal"], Method: methodT},
		napi.Property{Key: sym["kTestVoidMethodTInternal"], Method: voidMethodT},
		napi.Property{Key: toStringTagSymbol, Getter: toStringTag, Attributes: enumerable},
		napi.Property{Key: iteratorSymbol, Method: iterator},
	)
	cls, err := env.DefineClass("Test", newWrapTest, props...)
	if err != nil {
		return napi.Object{}, err
	}
	exports, err := env.NewObject()
	if err != nil {
		return napi.Object{}, err
	}
	return exports, exports.Set("Test", cls.Value)
}

// newWrapTest is the Test constructor: it wraps a new wrapTest, keeps the optional finalize
// callback and a one-byte external buffer alive with references, and defines two enumerable own
// accessors.
func newWrapTest(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	t := &wrapTest{}
	if c.Len() > 0 {
		f, err := argFunction(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		if t.finalizeCb, err = f.NewRef(1); err != nil {
			return napi.Value{}, err
		}
	}
	this := c.This()
	ownGetter := func(c napi.CallContext) (napi.Value, error) { return str(c.Env(), t.value) }
	if err := this.DefineProperties(
		napi.Property{Name: "ownProperty", Getter: ownGetter, Attributes: napi.Enumerable},
		napi.Property{Name: "ownPropertyT", Getter: ownGetter, Attributes: napi.Enumerable},
	); err != nil {
		return napi.Value{}, err
	}
	buf, err := env.NewExternalBuffer(make([]byte, 1), nil)
	if err != nil {
		return napi.Value{}, err
	}
	if t.bufRef, err = buf.NewRef(1); err != nil {
		return napi.Value{}, err
	}
	return napi.Value{}, this.Wrap(t, func(env napi.Env, v any) {
		t := v.(*wrapTest)
		t.bufRef.Delete()
		if t.finalizeCb.IsEmpty() {
			return
		}
		defer t.finalizeCb.Delete()
		cb, err := t.finalizeCb.Value()
		if err != nil {
			return
		}
		f, err := cb.AsFunction()
		if err != nil {
			return
		}
		f.Call(env.Global().Value, must(env.NewBoolean(true)))
	})
}

// dtorCalled counts the destruction of the native object of objectwrap_removewrap's Test.
var dtorCalled int

func initObjectWrapRemoveWrap(env napi.Env) (napi.Object, error) {
	// destroy is ~DtorCounter, which asserts it runs once.
	destroy := func() {
		if dtorCalled != 0 {
			napi.FatalError("objectwrap_removewrap", "native object destroyed twice")
		}
		dtorCalled++
	}
	// The constructor throws after wrapping, so ObjectWrap removes the wrap and destroys the
	// native object right away; the finalizer must then never run.
	ctor := func(c napi.CallContext) (napi.Value, error) {
		this := c.This()
		if err := this.Wrap(&struct{}{}, func(napi.Env, any) { destroy() }); err != nil {
			return napi.Value{}, err
		}
		if _, err := this.RemoveWrap(); err != nil {
			return napi.Value{}, err
		}
		destroy()
		e, err := newError(c.Env(), "Some error")
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, napi.Throw(e.Value)
	}
	cls, err := env.DefineClass("Test", ctor)
	if err != nil {
		return napi.Object{}, err
	}
	exports, err := newExports(env, fn{"getDtorCalled", func(c napi.CallContext) (napi.Value, error) {
		return c.Env().NewInt32(int32(dtorCalled))
	}})
	if err != nil {
		return napi.Object{}, err
	}
	return exports, exports.Set("Test", cls.Value)
}

func initObjectWrapConstructorException(env napi.Env) (napi.Object, error) {
	const name = "ConstructorExceptionTest"
	cls, err := env.DefineClass(name, func(c napi.CallContext) (napi.Value, error) {
		if err := c.This().Wrap(&struct{}{}, nil); err != nil {
			return napi.Value{}, err
		}
		e, err := newError(c.Env(), "an exception")
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, napi.Throw(e.Value)
	})
	if err != nil {
		return napi.Object{}, err
	}
	exports, err := env.NewObject()
	if err != nil {
		return napi.Object{}, err
	}
	return exports, exports.Set(name, cls.Value)
}
