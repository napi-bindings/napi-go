// Package napitest è l'equivalente Go di test/js-native-api/common.h e common-inl.h della suite di
// Node.js: le funzioni hanno lo stesso comportamento delle macro C e producono gli stessi
// messaggi, così gli script JS originali girano senza modifiche contro gli addon portati in Go.
//
// Corrispondenze:
//
//	NODE_API_CALL(env, call)              → if !napitest.Call(env, call) { return nil }
//	NODE_API_CALL_RETURN_VOID(env, call)  → if !napitest.Call(env, call) { return }
//	NODE_API_ASSERT(env, cond, msg)       → if !napitest.Assert(env, cond, "<cond in C>", msg) { return nil }
//	GET_AND_THROW_LAST_ERROR(env)         → napitest.GetAndThrowLastError(env)
//	DECLARE_NODE_API_PROPERTY(name, fn)   → napitest.DeclareProperty(name, fn)
//	DECLARE_NODE_API_GETTER(name, fn)     → napitest.DeclareGetter(name, fn)
//	DECLARE_NODE_API_PROPERTY_VALUE(n, v) → napitest.DeclarePropertyValue(n, v)
//	add_returned_status / add_last_status → napitest.AddReturnedStatus / napitest.AddLastStatus
//	NAPI_MODULE / NAPI_MODULE_INIT        → napitest.ModuleInit(init) chiamata in init()
//
// Il testo della condizione di Assert va copiato alla lettera dal sorgente C, perché la macro lo
// inserisce nel messaggio con lo stringify del preprocessore e gli script JS lo verificano.
//
// Le funzioni JS create da Go passano per sys.CallbackTrampoline: il campo data di napi
// trasporta l'indice della funzione Go registrata (Register), non il data del test C.
package napitest

/*
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Func è l'equivalente Go di napi_callback.
type Func func(env sys.Env, info sys.CallbackInfo) sys.Value

// Messaggi letterali di common.h / common-inl.h.
const (
	emptyErrorMessage   = "empty error message"
	lastStatusOK        = "napi_ok"
	exceptionKeySuffix  = "Exception"
	invalidStatusPrefix = "Invalid status ["
	invalidStatusSuffix = "]"
	unavailableMessage  = "napitest: Node-API function not exported by this Node.js"
)

// entry è una funzione registrata; argc è il numero di argomenti della chiamata JS e serve solo
// agli accessor, dove getter e setter condividono lo stesso data.
type entry func(env sys.Env, info sys.CallbackInfo, argc int) sys.Value

var (
	registryMu sync.Mutex
	// registry è copy-on-write: le letture dal CallbackHandler non prendono lock, anche quando un
	// worker registra funzioni durante il proprio Init. L'indice 0 è riservato: un data NULL non
	// corrisponde mai a una funzione.
	registry atomic.Pointer[[]entry]

	cstrings sync.Map // string → *byte in memoria C, mai liberata
)

func init() {
	reserved := []entry{nil}
	registry.Store(&reserved)
	sys.SetCallbackHandler(dispatchCallback)
	sys.SetAsyncExecuteHandler(dispatchAsyncExecute)
	sys.SetAsyncCompleteHandler(dispatchAsyncComplete)
	sys.SetFinalizeHandler(dispatchFinalize)
	sys.SetCallJSHandler(dispatchCallJS)
	sys.SetCleanupHookHandler(dispatchCleanupHook)
}

func dispatchCallback(env sys.Env, info sys.CallbackInfo, data uintptr, _ sys.Value, args []sys.Value) sys.Value {
	fns := *registry.Load()
	if data >= uintptr(len(fns)) || fns[data] == nil {
		throwMessage(env, "napitest: unknown callback index "+strconv.FormatUint(uint64(data), 10))
		return nil
	}
	return fns[data](env, info, len(args))
}

// ModuleInit registra la funzione di inizializzazione del modulo (Init di NAPI_MODULE). Va
// chiamata da init() del pacchetto main dell'addon.
func ModuleInit(fn func(env sys.Env, exports sys.Value) sys.Value) {
	sys.SetModuleInitHandler(fn)
}

func register(e entry) uintptr {
	registryMu.Lock()
	defer registryMu.Unlock()
	old := *registry.Load()
	next := make([]entry, len(old)+1)
	copy(next, old)
	next[len(old)] = e
	registry.Store(&next)
	return uintptr(len(old))
}

// Register rende fn chiamabile da JS e restituisce il valore da passare come data insieme a
// sys.CallbackTrampoline (napi_create_function, napi_define_class, descrittori di proprietà).
// Ogni chiamata aggiunge una voce: le funzioni create più volte (factory) vanno registrate una
// volta sola, a livello di pacchetto.
func Register(fn Func) uintptr {
	if fn == nil {
		panic("napitest: Register(nil)")
	}
	return register(func(env sys.Env, info sys.CallbackInfo, _ int) sys.Value { return fn(env, info) })
}

// RegisterAccessor registra una coppia getter/setter che Node-API chiama con lo stesso data: la
// chiamata senza argomenti è il getter, quella con un argomento il setter. setter può essere nil.
func RegisterAccessor(getter, setter Func) uintptr {
	if getter == nil {
		panic("napitest: RegisterAccessor with nil getter")
	}
	return register(func(env sys.Env, info sys.CallbackInfo, argc int) sys.Value {
		if argc == 0 || setter == nil {
			return getter(env, info)
		}
		return setter(env, info)
	})
}

// CStr restituisce s terminata da NUL in memoria C. Le stringhe sono condivise e non vengono mai
// liberate: vanno usate solo per nomi e costanti, così da poter stare anche dentro strutture
// passate a C (es. sys.PropertyDescriptor) senza violare le regole di cgo sui puntatori.
func CStr(s string) *byte {
	if p, ok := cstrings.Load(s); ok {
		return p.(*byte)
	}
	p := (*byte)(unsafe.Pointer(C.CString(s)))
	if prev, loaded := cstrings.LoadOrStore(s, p); loaded {
		C.free(unsafe.Pointer(p))
		return prev.(*byte)
	}
	return p
}

// GoString copia una stringa C terminata da NUL; nil diventa "".
func GoString(p *byte) string {
	if p == nil {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(p)))
}

// withCString chiama fn con una copia temporanea di s in memoria C.
func withCString(s string, fn func(p *byte)) {
	p := C.CString(s)
	defer C.free(unsafe.Pointer(p))
	fn((*byte)(unsafe.Pointer(p)))
}

// ThrowTypeError equivale a napi_throw_type_error(env, NULL, msg) con lo stesso ripiego su
// stderr di throw.
func ThrowTypeError(env sys.Env, msg string) {
	withCString(msg, func(p *byte) {
		if st := sys.ThrowTypeError(env, nil, p); st != sys.StatusOK {
			fmt.Fprintf(os.Stderr, "napitest: napi_throw_type_error(%q) failed: %s\n", msg, st)
		}
	})
}

func throwMessage(env sys.Env, msg string) {
	withCString(msg, func(p *byte) { throw(env, p) })
}

// throw lancia un Error con msg; se napi_throw_error fallisce il messaggio resta almeno su
// stderr, così il test fallisce con una causa leggibile invece che in silenzio.
func throw(env sys.Env, msg *byte) {
	if st := sys.ThrowError(env, nil, msg); st != sys.StatusOK {
		fmt.Fprintf(os.Stderr, "napitest: napi_throw_error(%q) failed: %s\n", GoString(msg), st)
	}
}

// GetAndThrowLastError equivale a GET_AND_THROW_LAST_ERROR: lancia un Error con il messaggio
// dell'ultimo errore Node-API, se non c'è già un'eccezione pendente.
func GetAndThrowLastError(env sys.Env) {
	var info *sys.ExtendedErrorInfo
	var msg *byte
	// Se la lettura fallisce info resta nil e si usa "empty error message", come nella macro C.
	if sys.GetLastErrorInfo(env, &info) == sys.StatusOK && info != nil {
		msg = info.ErrorMessage
	}
	var pending bool
	// Se il controllo fallisce si lancia comunque l'errore: meglio un'eccezione in più che
	// perderne una.
	if sys.IsExceptionPending(env, &pending) != sys.StatusOK {
		pending = false
	}
	// Se un'eccezione è già pendente non la si sostituisce.
	if pending {
		return
	}
	if msg == nil {
		msg = CStr(emptyErrorMessage)
	}
	throw(env, msg)
}

// Call equivale a NODE_API_CALL_BASE: se status non è StatusOK lancia l'ultimo errore e
// restituisce false, e il chiamante deve uscire come farebbe la macro.
func Call(env sys.Env, status sys.Status) bool {
	if status == sys.StatusUnavailable {
		// Non è uno status di Node-API: la funzione manca dal Node in esecuzione.
		throwMessage(env, unavailableMessage)
		return false
	}
	if status != sys.StatusOK {
		GetAndThrowLastError(env)
		return false
	}
	return true
}

// Assert equivale a NODE_API_ASSERT_BASE. expr è il testo C della condizione, come lo produce
// lo stringify della macro.
func Assert(env sys.Env, cond bool, expr, message string) bool {
	if !cond {
		throwMessage(env, "assertion ("+expr+") failed: "+message)
		return false
	}
	return true
}

// AssertStatus equivale a NODE_API_ASSERT_STATUS: se cond è falsa lancia l'errore di Assert e
// restituisce StatusGenericFailure, altrimenti StatusOK.
func AssertStatus(env sys.Env, cond bool, expr, message string) sys.Status {
	if !Assert(env, cond, expr, message) {
		return sys.StatusGenericFailure
	}
	return sys.StatusOK
}

// DeclareProperty equivale a DECLARE_NODE_API_PROPERTY.
func DeclareProperty(name string, fn Func) sys.PropertyDescriptor {
	return sys.PropertyDescriptor{
		Utf8Name:   CStr(name),
		Method:     sys.CallbackTrampoline,
		Attributes: sys.PropertyDefault,
		Data:       Register(fn),
	}
}

// DeclareGetter equivale a DECLARE_NODE_API_GETTER.
func DeclareGetter(name string, fn Func) sys.PropertyDescriptor {
	return sys.PropertyDescriptor{
		Utf8Name:   CStr(name),
		Getter:     sys.CallbackTrampoline,
		Attributes: sys.PropertyDefault,
		Data:       Register(fn),
	}
}

// DeclarePropertyValue equivale a DECLARE_NODE_API_PROPERTY_VALUE.
func DeclarePropertyValue(name string, value sys.Value) sys.PropertyDescriptor {
	return sys.PropertyDescriptor{
		Utf8Name:   CStr(name),
		Value:      value,
		Attributes: sys.PropertyDefault,
	}
}

// Method equivale a { name, NULL, fn, NULL, NULL, NULL, attributes, NULL }.
func Method(name string, fn Func, attributes sys.PropertyAttributes) sys.PropertyDescriptor {
	d := DeclareProperty(name, fn)
	d.Attributes = attributes
	return d
}

// Value equivale a { name, NULL, NULL, NULL, NULL, value, attributes, NULL }.
func Value(name string, value sys.Value, attributes sys.PropertyAttributes) sys.PropertyDescriptor {
	d := DeclarePropertyValue(name, value)
	d.Attributes = attributes
	return d
}

// Accessor equivale a { name, NULL, NULL, getter, setter, NULL, attributes, NULL }; setter può
// essere nil. Getter e setter condividono il data, come in Node-API (vedi RegisterAccessor).
func Accessor(name string, getter, setter Func, attributes sys.PropertyAttributes) sys.PropertyDescriptor {
	d := sys.PropertyDescriptor{
		Utf8Name:   CStr(name),
		Getter:     sys.CallbackTrampoline,
		Attributes: attributes,
		Data:       RegisterAccessor(getter, setter),
	}
	if setter != nil {
		d.Setter = sys.CallbackTrampoline
	}
	return d
}

// DefineProperties chiama napi_define_properties su tutto props (sizeof(props)/sizeof(*props)
// in C). Con props vuoto passa un puntatore NULL.
func DefineProperties(env sys.Env, object sys.Value, props []sys.PropertyDescriptor) sys.Status {
	if len(props) == 0 {
		return sys.DefineProperties(env, object, 0, nil)
	}
	return sys.DefineProperties(env, object, uint(len(props)), &props[0])
}

// AddReturnedStatus equivale ad add_returned_status di common-inl.h: imposta object[key] a
// expectedMessage se actual == expected, altrimenti a "Invalid status [<actual>]".
func AddReturnedStatus(env sys.Env, key string, object sys.Value, expectedMessage string,
	expected, actual sys.Status) {
	msg := expectedMessage
	if actual != expected {
		msg = invalidStatusPrefix + strconv.Itoa(int(actual)) + invalidStatusSuffix
	}
	var prop sys.Value
	ok := true
	withCString(msg, func(p *byte) {
		ok = Call(env, sys.CreateStringUTF8(env, p, sys.AutoLength, &prop))
	})
	if !ok {
		return
	}
	withCString(key, func(p *byte) { Call(env, sys.SetNamedProperty(env, object, p, prop)) })
}

// AddLastStatus equivale ad add_last_status di common-inl.h: imposta returnValue[key] al
// messaggio dell'ultimo errore ("napi_ok" se assente) e, se c'è un'eccezione pendente, la
// rimuove e la salva in returnValue[key+"Exception"].
func AddLastStatus(env sys.Env, key string, returnValue sys.Value) {
	var last *sys.ExtendedErrorInfo
	if !Call(env, sys.GetLastErrorInfo(env, &last)) {
		return
	}
	// Il contenuto di last cambia alla prossima chiamata Node-API: si copia subito.
	message := lastStatusOK
	if last != nil && last.ErrorMessage != nil {
		message = GoString(last.ErrorMessage)
	}
	var pending bool
	if !Call(env, sys.IsExceptionPending(env, &pending)) {
		return
	}
	if pending {
		var exception sys.Value
		if !Call(env, sys.GetAndClearLastException(env, &exception)) {
			return
		}
		ok := true
		withCString(key+exceptionKeySuffix, func(p *byte) {
			ok = Call(env, sys.SetNamedProperty(env, returnValue, p, exception))
		})
		if !ok {
			return
		}
	}
	var prop sys.Value
	ok := true
	withCString(message, func(p *byte) {
		ok = Call(env, sys.CreateStringUTF8(env, p, sys.AutoLength, &prop))
	})
	if !ok {
		return
	}
	withCString(key, func(p *byte) { Call(env, sys.SetNamedProperty(env, returnValue, p, prop)) })
}
