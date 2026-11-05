package main

// Port of test/external.cc, test/reference.cc, test/handlescope.cc, test/type_taggable.cc,
// test/promise.cc, test/run_script.cc, test/memory_management.cc,
// test/version_management.cc, test/env_cleanup.cc, test/env_misc.cc and test/addon_data.cc.
//
// C++ constructs without a Go counterpart:
//   - the copy and move semantics of Napi::Reference are copies of the napi.Ref value;
//   - EscapableHandleScope::Escape called twice: napi.Env.WithEscapableScope escapes exactly the
//     value its function returns, so the double escape goes through sys to reach the same
//     Node-API error;
//   - Promise::Then and Promise::Catch call the JS then/catch methods, as node-addon-api does;
//   - the external data pointer is the Go value held by the external, and the finalize hint is
//     data captured by the Go finalizer.

import (
	"errors"
	"fmt"
	"os"

	"github.com/napi-bindings/napi-go/napi"
	"github.com/napi-bindings/napi-go/sys"
)

// ---- External ----

var (
	externalTestData      = 1
	externalFinalizeCount int
)

func initExternal(env napi.Env) (napi.Object, error) {
	withFinalizer := func(c napi.CallContext) (napi.Value, error) {
		externalFinalizeCount = 0
		data := 1
		x, err := c.Env().NewExternal(&data, func(napi.Env, any) { externalFinalizeCount++ })
		return x.Value, err
	}
	return newExports(env,
		fn{"createExternal", func(c napi.CallContext) (napi.Value, error) {
			externalFinalizeCount = 0
			x, err := c.Env().NewExternal(&externalTestData, nil)
			return x.Value, err
		}},
		fn{"createExternalWithFinalize", withFinalizer},
		fn{"createExternalWithFinalizeHint", withFinalizer},
		fn{"createExternalWithFinalizeException", func(c napi.CallContext) (napi.Value, error) {
			data := 1
			x, err := c.Env().NewExternal(&data, func(env napi.Env, _ any) {
				env.Throw(errors.New("Finalizer exception"))
			})
			return x.Value, err
		}},
		fn{"checkExternal", func(c napi.CallContext) (napi.Value, error) {
			if !c.Arg(0).IsExternal() {
				return napi.Value{}, errors.New("An external argument was expected.")
			}
			x, err := c.Arg(0).AsExternal()
			if err != nil {
				return napi.Value{}, err
			}
			v, err := x.Get()
			if err != nil {
				return napi.Value{}, err
			}
			if p, ok := v.(*int); !ok || p == nil || *p != 1 {
				return napi.Value{}, errors.New("An external value of 1 was expected.")
			}
			return napi.Value{}, nil
		}},
		fn{"getFinalizeCount", func(c napi.CallContext) (napi.Value, error) {
			return c.Env().NewInt32(int32(externalFinalizeCount))
		}},
	)
}

// ---- Reference ----

// weakBuffer is the static weak Reference<Buffer<uint8_t>> of reference.cc.
var weakBuffer napi.Ref

// check is the assert() of the C++ tests: a failed check is thrown as an Error.
func check(ok bool, what string) error {
	if !ok {
		return errors.New("assertion failed: " + what)
	}
	return nil
}

func initReference(env napi.Env) (napi.Object, error) {
	return newExports(env,
		fn{"createWeakArray", func(c napi.CallContext) (napi.Value, error) {
			buf, _, err := c.Env().NewBuffer(1)
			if err != nil {
				return napi.Value{}, err
			}
			weakBuffer, err = buf.NewRef(0)
			return napi.Value{}, err
		}},
		fn{"accessWeakArrayEmpty", func(c napi.CallContext) (napi.Value, error) {
			v, err := weakBuffer.Value()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(v.IsEmpty())
		}},
		fn{"refMoveAssignTest", func(c napi.CallContext) (napi.Value, error) {
			o, err := c.Env().NewObject()
			if err != nil {
				return napi.Value{}, err
			}
			if err := o.Set("tPro", must(str(c.Env(), "tTEST"))); err != nil {
				return napi.Value{}, err
			}
			ref, err := o.NewRef(0)
			if err != nil {
				return napi.Value{}, err
			}
			// The C++ test suppresses the destructor and never deletes the reference.
			existing := ref
			if err := check(existing == ref, "existingRef == ref"); err != nil {
				return napi.Value{}, err
			}
			v, err := existing.Value()
			if err != nil {
				return napi.Value{}, err
			}
			p, err := unsafeObject(v).Get("tPro")
			if err != nil {
				return napi.Value{}, err
			}
			s, err := p.GoString()
			if err != nil {
				return napi.Value{}, err
			}
			moved := existing
			return napi.Value{}, errors.Join(check(s == "tTEST", `val == "tTEST"`), check(moved == ref, "copyMoveRef == ref"))
		}},
		fn{"referenceRefTest", func(c napi.CallContext) (napi.Value, error) {
			o, err := c.Env().NewObject()
			if err != nil {
				return napi.Value{}, err
			}
			ref, err := o.NewRef(0)
			if err != nil {
				return napi.Value{}, err
			}
			defer ref.Delete()
			up, err := ref.Ref()
			if err != nil {
				return napi.Value{}, err
			}
			down, err := ref.Unref()
			if err != nil {
				return napi.Value{}, err
			}
			return napi.Value{}, errors.Join(check(up == 1, "ref.Ref() == 1"), check(down == 0, "ref.Unref() == 0"))
		}},
		fn{"refResetTest", func(c napi.CallContext) (napi.Value, error) {
			env := c.Env()
			o, err := env.NewObject()
			if err != nil {
				return napi.Value{}, err
			}
			ref, err := o.NewRef(0)
			if err != nil {
				return napi.Value{}, err
			}
			if err := check(!ref.IsEmpty(), "!ref.IsEmpty()"); err != nil {
				return napi.Value{}, err
			}
			// Reset(): delete the reference and clear the handle.
			if err := ref.Delete(); err != nil {
				return napi.Value{}, err
			}
			ref = napi.Ref{}
			if err := check(ref.IsEmpty(), "ref.IsEmpty()"); err != nil {
				return napi.Value{}, err
			}
			// Reset(newObject, 1).
			n, err := env.NewObject()
			if err != nil {
				return napi.Value{}, err
			}
			if err := n.Set("n-api", must(str(env, "node"))); err != nil {
				return napi.Value{}, err
			}
			if ref, err = n.NewRef(1); err != nil {
				return napi.Value{}, err
			}
			defer ref.Delete()
			v, err := ref.Value()
			if err != nil {
				return napi.Value{}, err
			}
			p, err := unsafeObject(v).Get("n-api")
			if err != nil {
				return napi.Value{}, err
			}
			s, err := p.GoString()
			if err != nil {
				return napi.Value{}, err
			}
			return napi.Value{}, errors.Join(check(!ref.IsEmpty(), "!ref.IsEmpty()"), check(s == "node", `val == "node"`))
		}},
	)
}

// ---- HandleScope ----

// stressEscapeLoops is LOOP_MAX in handlescope.cc.
const stressEscapeLoops = 1_000_000

func initHandleScope(env napi.Env) (napi.Object, error) {
	createScope := func(inner, outer string) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			env := c.Env()
			if err := env.WithScope(func() error {
				_, err := env.NewString(inner)
				return err
			}); err != nil {
				return napi.Value{}, err
			}
			return str(env, outer)
		}
	}
	escape := func(inner string) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			env := c.Env()
			return env.WithEscapableScope(func() (napi.Value, error) { return str(env, inner) })
		}
	}
	return newExports(env,
		fn{"createScope", createScope("inner-scope", "scope")},
		// HandleScope(env, existing) adopts a scope opened with napi_open_handle_scope: in Go the
		// scope is always opened by WithScope.
		fn{"createScopeFromExisting", createScope("inner-existing-scope", "existing_scope")},
		fn{"escapeFromScope", escape("inner-scope")},
		fn{"escapeFromExistingScope", escape("inner-existing-scope")},
		fn{"stressEscapeFromScope", func(c napi.CallContext) (napi.Value, error) {
			env := c.Env()
			var result napi.Value
			for i := 0; i < stressEscapeLoops; i++ {
				v, err := env.WithEscapableScope(func() (napi.Value, error) {
					v, err := str(env, fmt.Sprintf("inner-scope%d", i))
					if err != nil || i != stressEscapeLoops-1 {
						return napi.Value{}, err
					}
					return v, nil
				})
				if err != nil {
					return napi.Value{}, err
				}
				if !v.IsEmpty() {
					result = v
				}
			}
			return result, nil
		}},
		fn{"doubleEscapeFromScope", doubleEscapeFromScope},
	)
}

func doubleEscapeFromScope(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	raw := env.Raw()
	var scope sys.EscapableHandleScope
	if err := env.Check(sys.OpenEscapableHandleScope(raw, &scope)); err != nil {
		return napi.Value{}, err
	}
	var escaped sys.Value
	v := must(str(env, "inner-scope"))
	err := env.Check(sys.EscapeHandle(raw, scope, v.Raw(), &escaped))
	if err == nil {
		v = must(str(env, "inner-scope"))
		err = env.Check(sys.EscapeHandle(raw, scope, v.Raw(), &escaped))
	}
	if cerr := env.Check(sys.CloseEscapableHandleScope(raw, scope)); err == nil {
		err = cerr
	}
	return napi.ValueFromRaw(raw, escaped), err
}

// ---- TypeTaggable ----

var typeTags = [...]napi.TypeTag{
	{Lower: 0xdaf987b3cc62481a, Upper: 0xb745b0497f299531},
	{Lower: 0xbb7936c374084d9b, Upper: 0xa9548d0762eeedb9},
	{Lower: 0xa5ed9ce2e4c00c38, Upper: 0},
	{Lower: 0, Upper: 0},
	{Lower: 0xa5ed9ce2e4c00c38, Upper: 0xdaf987b3cc62481a},
}

// externalTagData is the non-null data of the tagged externals (External<void>::New(env, 0x1)).
var externalTagData = struct{}{}

// taggable abstracts napi.Object and napi.External, the two Node-API type-taggable values.
type taggable interface {
	SetTypeTag(napi.TypeTag) error
	HasTypeTag(napi.TypeTag) (bool, error)
}

func initTypeTaggable(env napi.Env) (napi.Object, error) {
	argTag := func(c napi.CallContext) (napi.TypeTag, error) {
		i, err := c.Arg(0).Int32()
		if err != nil {
			return napi.TypeTag{}, err
		}
		if i < 0 || int(i) >= len(typeTags) {
			return napi.TypeTag{}, napi.RangeError("", "type tag index out of range")
		}
		return typeTags[i], nil
	}
	suite := func(create func(env napi.Env) (napi.Value, taggable, error), unsafeAs func(napi.Value) taggable) (napi.Object, error) {
		return newExports(env,
			fn{"typeTaggedInstance", func(c napi.CallContext) (napi.Value, error) {
				tag, err := argTag(c)
				if err != nil {
					return napi.Value{}, err
				}
				v, t, err := create(c.Env())
				if err != nil {
					return napi.Value{}, err
				}
				return v, t.SetTypeTag(tag)
			}},
			fn{"checkTypeTag", func(c napi.CallContext) (napi.Value, error) {
				tag, err := argTag(c)
				if err != nil {
					return napi.Value{}, err
				}
				ok, err := unsafeAs(c.Arg(1)).HasTypeTag(tag)
				if err != nil {
					return napi.Value{}, err
				}
				return c.Env().NewBoolean(ok)
			}},
		)
	}
	external, err := suite(func(env napi.Env) (napi.Value, taggable, error) {
		x, err := env.NewExternal(&externalTagData, nil)
		return x.Value, x, err
	}, func(v napi.Value) taggable { return napi.External{Value: v} })
	if err != nil {
		return napi.Object{}, err
	}
	object, err := suite(func(env napi.Env) (napi.Value, taggable, error) {
		o, err := env.NewObject()
		return o.Value, o, err
	}, func(v napi.Value) taggable { return unsafeObject(v) })
	if err != nil {
		return napi.Object{}, err
	}
	exports, err := env.NewObject()
	if err != nil {
		return napi.Object{}, err
	}
	return exports, setProps(exports, map[string]napi.Value{"external": external.Value, "object": object.Value})
}

// ---- Promise ----

// promiseResolution is the value the then/catch tests settle their promise with.
const promiseResolution = 42

func initPromise(env napi.Env) (napi.Object, error) {
	settled := func(resolve bool) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			p, d, err := c.Env().NewPromise()
			if err != nil {
				return napi.Value{}, err
			}
			if resolve {
				err = d.Resolve(c.Arg(0))
			} else {
				err = d.Reject(c.Arg(0))
			}
			return p.Value, err
		}
	}
	// chained calls promise.method(handlers...) on a new promise, then settles it and returns
	// { isPromise, promise } for the promise returned by the method.
	chained := func(method string, handlers int, resolve bool) napi.Callback {
		return func(c napi.CallContext) (napi.Value, error) {
			env := c.Env()
			p, d, err := env.NewPromise()
			if err != nil {
				return napi.Value{}, err
			}
			args := make([]napi.Value, handlers)
			for i := range args {
				f, err := argFunction(c, i)
				if err != nil {
					return napi.Value{}, err
				}
				args[i] = f.Value
			}
			m, err := p.Get(method)
			if err != nil {
				return napi.Value{}, err
			}
			mf, err := m.AsFunction()
			if err != nil {
				return napi.Value{}, err
			}
			result, err := mf.Call(p.Value, args...)
			if err != nil {
				return napi.Value{}, err
			}
			if resolve {
				err = d.Resolve(must(env.NewInt32(promiseResolution)))
			} else {
				err = d.Reject(must(str(env, "Rejected")))
			}
			if err != nil {
				return napi.Value{}, err
			}
			o, err := env.NewObject()
			if err != nil {
				return napi.Value{}, err
			}
			return o.Value, setProps(o, map[string]napi.Value{
				"isPromise": must(env.NewBoolean(result.IsPromise())),
				"promise":   result,
			})
		}
	}
	return newExports(env,
		fn{"isPromise", func(c napi.CallContext) (napi.Value, error) { return c.Env().NewBoolean(c.Arg(0).IsPromise()) }},
		fn{"resolvePromise", settled(true)},
		fn{"rejectPromise", settled(false)},
		fn{"promiseReturnsCorrectEnv", func(c napi.CallContext) (napi.Value, error) {
			p, _, err := c.Env().NewPromise()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewBoolean(p.Env() == c.Env())
		}},
		fn{"thenMethodOnFulfilled", chained("then", 1, true)},
		fn{"thenMethodOnFulfilledOnRejectedResolve", chained("then", 2, true)},
		fn{"thenMethodOnFulfilledOnRejectedReject", chained("then", 2, false)},
		fn{"catchMethod", chained("catch", 1, false)},
	)
}

// ---- RunScript ----

func initRunScript(env napi.Env) (napi.Object, error) {
	const sumScript = "1 + 2 + 3"
	run := func(c napi.CallContext) (napi.Value, error) { return c.Env().RunScript(sumScript) }
	return newExports(env,
		fn{"plainString", run},
		fn{"stdString", run},
		fn{"jsString", func(c napi.CallContext) (napi.Value, error) {
			src, err := argString(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().RunScript(src)
		}},
		fn{"withContext", func(c napi.CallContext) (napi.Value, error) {
			body, err := argString(c, 0)
			if err != nil {
				return napi.Value{}, err
			}
			ctx, err := argObject(c, 1)
			if err != nil {
				return napi.Value{}, err
			}
			keys, err := ctx.Keys()
			if err != nil {
				return napi.Value{}, err
			}
			n, err := keys.Len()
			if err != nil {
				return napi.Value{}, err
			}
			code := "("
			args := make([]napi.Value, 0, n)
			for i := uint32(0); i < n; i++ {
				k, err := keys.GetIndex(i)
				if err != nil {
					return napi.Value{}, err
				}
				name, err := k.GoString()
				if err != nil {
					return napi.Value{}, err
				}
				if i != 0 {
					code += ","
				}
				code += name
				v, err := ctx.GetProperty(k)
				if err != nil {
					return napi.Value{}, err
				}
				args = append(args, v)
			}
			fnValue, err := c.Env().RunScript(code + ") => " + body)
			if err != nil {
				return napi.Value{}, err
			}
			f, err := fnValue.AsFunction()
			if err != nil {
				return napi.Value{}, err
			}
			return f.Call(napi.Value{}, args...)
		}},
	)
}

// ---- MemoryManagement, VersionManagement, env_misc ----

// externalMemoryBytes is kSize in memory_management.cc.
const externalMemoryBytes = 1024 * 1024

func initMemoryManagement(env napi.Env) (napi.Object, error) {
	return newExports(env, fn{"externalAllocatedMemory", func(c napi.CallContext) (napi.Value, error) {
		env := c.Env()
		baseline, err := env.AdjustExternalMemory(0)
		if err != nil {
			return napi.Value{}, err
		}
		if _, err := env.AdjustExternalMemory(externalMemoryBytes); err != nil {
			return napi.Value{}, err
		}
		after, err := env.AdjustExternalMemory(-externalMemoryBytes)
		if err != nil {
			return napi.Value{}, err
		}
		return env.NewBoolean(after == baseline)
	}})
}

func initVersionManagement(env napi.Env) (napi.Object, error) {
	return newExports(env,
		fn{"getNapiVersion", func(c napi.CallContext) (napi.Value, error) {
			v, err := c.Env().NapiVersion()
			if err != nil {
				return napi.Value{}, err
			}
			return c.Env().NewUint32(v)
		}},
		fn{"getNodeVersion", func(c napi.CallContext) (napi.Value, error) {
			env := c.Env()
			v, err := env.NodeVersion()
			if err != nil {
				return napi.Value{}, err
			}
			o, err := env.NewObject()
			if err != nil {
				return napi.Value{}, err
			}
			return o.Value, setProps(o, map[string]napi.Value{
				"major":   must(env.NewUint32(v.Major)),
				"minor":   must(env.NewUint32(v.Minor)),
				"patch":   must(env.NewUint32(v.Patch)),
				"release": must(str(env, v.Release)),
			})
		}},
	)
}

func initEnvMiscellaneous(env napi.Env) (napi.Object, error) {
	return newExports(env, fn{"get_module_file_name", func(c napi.CallContext) (napi.Value, error) {
		name, err := c.Env().ModuleFileName()
		if err != nil {
			return napi.Value{}, err
		}
		return str(c.Env(), name)
	}})
}

// ---- Env cleanup hooks ----

// The arguments printed by the cleanup hooks (secret1 and secret2 in env_cleanup.cc).
const (
	cleanupSecret1 = 42
	cleanupSecret2 = 43
)

func initEnvCleanup(env napi.Env) (napi.Object, error) {
	// The hooks print to stdout, which test/env_cleanup.js reads from the child process.
	printf := func(format string, args ...any) func() {
		return func() { fmt.Fprintf(os.Stdout, format, args...) }
	}
	return newExports(env, fn{"addHooks", func(c napi.CallContext) (napi.Value, error) {
		env := c.Env()
		shouldRemove, err := argBool(c, 0)
		if err != nil {
			return napi.Value{}, err
		}
		// The C++ overloads (function pointer with or without argument, lambdas) are all a Go
		// func(); hooks run in reverse order.
		fns := []func(){
			printf("static cleanup(%d)\n", cleanupSecret1),
			printf("static cleanup(%d)\n", cleanupSecret1),
			printf("static cleanup(%d)\n", cleanupSecret2),
			printf("static cleanup()\n"),
			printf("static cleanup()\n"),
			printf("lambda cleanup(%d)\n", cleanupSecret1),
			printf("lambda cleanup(void)\n"),
			printf("lambda cleanup()\n"),
		}
		hooks := make([]napi.CleanupHook, 0, len(fns))
		for _, f := range fns {
			h, err := env.AddCleanupHook(f)
			if err != nil {
				return napi.Value{}, err
			}
			hooks = append(hooks, h)
		}
		added := len(hooks)
		if shouldRemove {
			for _, h := range hooks {
				if err := h.Remove(); err != nil {
					return napi.Value{}, err
				}
			}
			added = 0
		}
		// A hook stored and removed right away (TestClass in the C++ test).
		h, err := env.AddCleanupHook(printf("static cleanup(%d)\n", cleanupSecret1))
		if err != nil {
			return napi.Value{}, err
		}
		if err := h.Remove(); err != nil {
			return napi.Value{}, err
		}
		return env.NewInt32(int32(added))
	}})
}

// ---- Addon data ----

// addonData is the instance data of addon_data.cc (class Addon).
type addonData struct {
	verbose          bool
	verboseIndicator napi.Ref // the VerboseIndicator class
}

func instanceAddon(env napi.Env) (*addonData, error) {
	v, err := env.InstanceData()
	if err != nil {
		return nil, err
	}
	a, ok := v.(*addonData)
	if !ok {
		return nil, errors.New("addon_data: no instance data")
	}
	return a, nil
}

// initAddonData exports the AddonFactory function itself, as InitAddonData does.
func initAddonData(env napi.Env) (napi.Object, error) {
	f, err := env.NewFunction("addon_data", addonFactory)
	return f.Object, err
}

func addonFactory(c napi.CallContext) (napi.Value, error) {
	env := c.Env()
	if !c.Arg(0).IsNumber() {
		return napi.Value{}, errors.New("Expected number")
	}
	hint, err := c.Arg(0).Uint32()
	if err != nil {
		return napi.Value{}, err
	}
	addon := &addonData{}
	// VerboseIndicator: instances read and write the verbose flag of the instance data.
	verboseGetter := func(c napi.CallContext) (napi.Value, error) {
		a, err := instanceAddon(c.Env())
		if err != nil {
			return napi.Value{}, err
		}
		return c.Env().NewBoolean(a.verbose)
	}
	verboseSetter := func(c napi.CallContext) (napi.Value, error) {
		a, err := instanceAddon(c.Env())
		if err != nil {
			return napi.Value{}, err
		}
		a.verbose, err = argBool(c, 0)
		return napi.Value{}, err
	}
	cls, err := env.DefineClass("VerboseIndicator", func(c napi.CallContext) (napi.Value, error) {
		if err := c.This().Wrap(&struct{}{}, nil); err != nil {
			return napi.Value{}, err
		}
		a, err := instanceAddon(c.Env())
		if err != nil {
			return napi.Value{}, err
		}
		return napi.Value{}, c.This().Set("verbose", must(c.Env().NewBoolean(a.verbose)))
	}, napi.Property{Name: "verbose", Getter: verboseGetter, Setter: verboseSetter})
	if err != nil {
		return napi.Value{}, err
	}
	if addon.verboseIndicator, err = cls.NewRef(1); err != nil {
		return napi.Value{}, err
	}
	// ~Addon, followed by the hint when one was given (DeleteAddon).
	err = env.SetInstanceData(addon, func(_ napi.Env, v any) {
		if v.(*addonData).verbose {
			fmt.Fprintln(os.Stderr, "addon_data: Addon::~Addon")
		}
		if hint != 0 {
			fmt.Fprintf(os.Stderr, "hint: %d\n", hint)
		}
	})
	if err != nil {
		return napi.Value{}, err
	}
	result, err := env.NewObject()
	if err != nil {
		return napi.Value{}, err
	}
	return result.Value, result.DefineProperties(napi.Property{
		Name: "verbose",
		Getter: func(c napi.CallContext) (napi.Value, error) {
			a, err := instanceAddon(c.Env())
			if err != nil {
				return napi.Value{}, err
			}
			ctor, err := a.verboseIndicator.Value()
			if err != nil {
				return napi.Value{}, err
			}
			f, err := ctor.AsFunction()
			if err != nil {
				return napi.Value{}, err
			}
			o, err := f.New()
			return o.Value, err
		},
		Setter: verboseSetter,
	})
}
