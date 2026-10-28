package napitest

import (
	"runtime/cgo"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// CallJSFunc equivale a napi_threadsafe_function_call_js. env e jsCallback sono nil quando la
// TSFN viene distrutta con elementi ancora in coda.
type CallJSFunc func(env sys.Env, jsCallback sys.Value, context, data uintptr)

type threadsafeFunction struct {
	callJS   CallJSFunc
	finalize FinalizeFunc
	context  uintptr
}

// ThreadsafeFunction prepara i tre argomenti di napi_create_threadsafe_function che nel C sono
// funzioni o il context: call_js_cb, thread_finalize_cb e context. callJS e finalize possono
// essere nil come nel C; context è il context del test C, che callJS, finalize e
// GetThreadsafeFunctionContext ricevono invariato.
//
// Il thread_finalize_cb è sempre sys.FinalizeTrampoline, anche quando finalize è nil (in quel caso
// non fa nulla). Il token restituito come context non viene mai rilasciato: Node-API chiama
// call_js_cb con env NULL per gli elementi rimasti in coda anche dopo il thread_finalize_cb e non
// segnala la fine, quindi non c'è un momento sicuro per farlo. È un handle per TSFN, accettabile
// nei test. Se napi_create_threadsafe_function fallisce va rilasciato con
// DiscardThreadsafeFunction.
//
// Con callJS nil call_js_cb è NULL e Node-API chiama la funzione JS senza argomenti.
func ThreadsafeFunction(callJS CallJSFunc, finalize FinalizeFunc,
	context uintptr) (sys.ThreadsafeFunctionCallJS, sys.Finalize, uintptr) {
	token := uintptr(cgo.NewHandle(&threadsafeFunction{callJS: callJS, finalize: finalize, context: context}))
	var callJSCb sys.ThreadsafeFunctionCallJS
	if callJS != nil {
		callJSCb = sys.CallJSTrampoline
	}
	return callJSCb, sys.FinalizeTrampoline, token
}

// DiscardThreadsafeFunction rilascia il context di ThreadsafeFunction di una TSFN mai creata.
func DiscardThreadsafeFunction(token uintptr) {
	cgo.Handle(token).Delete()
}

// GetThreadsafeFunctionContext equivale a napi_get_threadsafe_function_context: restituisce il
// context originale passato a ThreadsafeFunction.
func GetThreadsafeFunctionContext(fn sys.ThreadsafeFunction, result *uintptr) sys.Status {
	var p unsafe.Pointer
	if st := sys.GetThreadsafeFunctionContext(fn, &p); st != sys.StatusOK {
		return st
	}
	*result = cgo.Handle(uintptr(p)).Value().(*threadsafeFunction).context
	return sys.StatusOK
}

func dispatchCallJS(env sys.Env, jsCallback sys.Value, context, data uintptr) {
	t := cgo.Handle(context).Value().(*threadsafeFunction)
	t.callJS(env, jsCallback, t.context, data)
}
