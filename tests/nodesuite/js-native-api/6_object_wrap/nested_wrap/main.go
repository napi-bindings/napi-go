// Porting di test/js-native-api/6_object_wrap/nested_wrap.cc e nested_wrap.h (target nested_wrap,
// NAPI_VERSION=10): un napi_ref annidato in un ObjectWrap, cancellato dal finalizer mentre il suo
// finalizer è già in programma.
package main

import (
	"runtime/cgo"

	"github.com/napi-bindings/napi-go/sys"
	nt "github.com/napi-bindings/napi-go/tests/nodesuite/internal/napitest"
)

func main() {}

func init() { nt.ModuleInit(initModule) }

// nestedWrap è la classe C++ NestedWrap; il puntatore passato a Node-API è un cgo.Handle.
type nestedWrap struct {
	env     sys.Env
	wrapper sys.Ref
	nested  sys.Ref
}

var (
	// constructor è NestedWrap::constructor.
	constructor       sys.Ref
	finalizationCount int32
	// newData è il data del costruttore: si registra una volta sola.
	newData = nt.Register(newNestedWrap)
)

// release equivale a ~NestedWrap: cancella anche il riferimento annidato. Gli status non sono
// controllati, come nel C.
func (o *nestedWrap) release() {
	sys.DeleteReference(o.env, o.wrapper)
	sys.DeleteReference(o.env, o.nested)
}

// destructor è NestedWrap::Destructor. Una volta chiamato annulla, cancellando i riferimenti,
// tutti i finalizer dell'oggetto ancora in programma: il loro nt.Finalizer non viene più
// chiamato e resta allocato, come accetta il test C.
func destructor(_ sys.Env, nativeObject, _ uintptr) {
	h := cgo.Handle(nativeObject)
	obj := h.Value().(*nestedWrap)
	h.Delete()
	obj.release()
	finalizationCount++
}

func initClass(env sys.Env, exports sys.Value) {
	var cons sys.Value
	if !nt.Call(env, sys.DefineClass(env, nt.CStr("NestedWrap"), sys.AutoLength, sys.CallbackTrampoline,
		newData, 0, nil, &cons)) {
		return
	}

	if !nt.Call(env, sys.CreateReference(env, cons, 1, &constructor)) {
		return
	}

	if !nt.Call(env, sys.SetNamedProperty(env, exports, nt.CStr("NestedWrap"), cons)) {
		return
	}
}

// newNestedWrap è NestedWrap::New.
func newNestedWrap(env sys.Env, info sys.CallbackInfo) sys.Value {
	var newTarget sys.Value
	if !nt.Call(env, sys.GetNewTarget(env, info, &newTarget)) {
		return nil
	}
	isConstructor := newTarget != nil
	if !nt.BasicAssert(isConstructor, "is_constructor", "Constructor called without new") {
		return nil
	}

	var this sys.Value
	if !nt.Call(env, sys.GetCbInfo(env, info, nil, nil, &this, nil)) {
		return nil
	}

	obj := &nestedWrap{env: env}
	h := cgo.NewHandle(obj)
	cb, hint := nt.Finalizer(destructor, 0)
	if !nt.Call(env, sys.Wrap(env, this, uintptr(h), cb, hint, &obj.wrapper)) {
		nt.DiscardFinalizer(hint)
		h.Delete()
		return nil
	}

	// Un secondo napi_ref, da cancellare nel distruttore.
	cb, hint = nt.Finalizer(destructor, 0)
	if !nt.Call(env, sys.AddFinalizer(env, this, uintptr(h), cb, hint, &obj.nested)) {
		nt.DiscardFinalizer(hint)
		return nil
	}

	return this
}

func getFinalizerCallCount(env sys.Env, _ sys.CallbackInfo) sys.Value {
	var result sys.Value
	if !nt.Call(env, sys.CreateInt32(env, finalizationCount, &result)) {
		return nil
	}
	return result
}

func initModule(env sys.Env, exports sys.Value) sys.Value {
	initClass(env, exports)

	descriptors := []sys.PropertyDescriptor{
		nt.DeclareProperty("getFinalizerCallCount", getFinalizerCallCount),
	}
	if !nt.Call(env, nt.DefineProperties(env, exports, descriptors)) {
		return nil
	}

	return exports
}
