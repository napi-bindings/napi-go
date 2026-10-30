package napi

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Env è l'ambiente Node-API di una callback (napi_env, Napi::Env). È valido solo sul thread
// JavaScript che lo ha fornito: non va passato a goroutine. Un Env resta lo stesso per tutta la
// vita del suo ambiente (main thread o worker_thread), quindi si può conservare insieme a una Ref,
// ma va usato solo da callback dello stesso thread.
type Env struct{ raw sys.Env }

// EnvFromRaw avvolge un napi_env di sys (per chi mescola napi e sys).
func EnvFromRaw(raw sys.Env) Env { return Env{raw: raw} }

// Raw restituisce il napi_env di sys.
func (e Env) Raw() sys.Env { return e.raw }

// InitFunc inizializza il modulo per un env e restituisce il valore delle export. Un Value vuoto
// lascia exports; un altro valore lo sostituisce (come module.exports = ...).
type InitFunc func(env Env, exports Object) (Value, error)

var inits struct {
	mu  sync.Mutex
	fns []InitFunc
}

// Init registra una funzione di inizializzazione del modulo. Va chiamata da init(): Node chiama
// le funzioni registrate, nell'ordine di registrazione, per ogni env che carica l'addon (anche nei
// worker_thread). Ognuna riceve le export restituite dalla precedente. Un errore fa fallire
// require(). Senza nessuna funzione registrata require() fallisce con il messaggio di Node.js per
// un modulo senza entry point.
func Init(fn InitFunc) {
	if fn == nil {
		panic("napi: Init(nil)")
	}
	inits.mu.Lock()
	inits.fns = append(inits.fns, fn)
	inits.mu.Unlock()
}

// noEntryPointMessage è il messaggio di Node.js per un modulo senza funzione di init.
const noEntryPointMessage = "Module has no declared entry point."

// envState è lo stato di napi per un env, raggiungibile da Node-API tramite napi_instance_data.
type envState struct {
	instanceData      any
	instanceFinalizer FinalizeFunc
}

var envStates table[envState]

func registerModule(env sys.Env, exports sys.Value) sys.Value {
	inits.mu.Lock()
	fns := inits.fns
	inits.mu.Unlock()
	if len(fns) == 0 {
		throwError(env, &Error{Code: sys.CodeNoHandler, Message: noEntryPointMessage})
		return nil
	}
	if err := attachEnvState(env); err != nil {
		throwError(env, err)
		return nil
	}
	e := Env{raw: env}
	ex := Value{env: env, raw: exports}
	for _, fn := range fns {
		v, err := fn(e, Object{ex})
		if err != nil {
			throwError(env, err)
			return nil
		}
		if v.raw != nil {
			ex = v
		}
	}
	return ex.raw
}

// attachEnvState crea lo stato dell'env e lo affida a napi_set_instance_data: Node chiama il
// finalizer quando l'env viene distrutto (uscita del processo o fine di un worker).
func attachEnvState(env sys.Env) error {
	st := &envState{}
	id := envStates.add(st)
	fid := handles.add(&handle{onFinalize: func(e Env, _ any) {
		s := envStates.remove(id)
		if s != nil && s.instanceFinalizer != nil {
			s.instanceFinalizer(e, s.instanceData)
		}
	}})
	if err := check(env, sys.SetInstanceData(env, id, sys.FinalizeTrampoline, fid)); err != nil {
		handles.remove(fid)
		envStates.remove(id)
		return err
	}
	return nil
}

// state restituisce lo stato dell'env.
func (e Env) state() (*envState, error) {
	var p unsafe.Pointer
	if err := check(e.raw, sys.GetInstanceData(e.raw, &p)); err != nil {
		return nil, err
	}
	st := envStates.get(uintptr(p))
	if st == nil {
		return nil, &Error{Code: sys.CodeInternal, Message: "env state not found (instance data replaced through sys?)"}
	}
	return st, nil
}

// SetInstanceData associa v all'env (napi_set_instance_data). Ogni env (main thread, worker) ha
// il proprio. Se fin non è nil viene chiamata alla distruzione dell'env con l'ultimo valore
// impostato; come in Node-API, sostituire il valore non chiama il finalizer precedente.
func (e Env) SetInstanceData(v any, fin FinalizeFunc) error {
	st, err := e.state()
	if err != nil {
		return err
	}
	st.instanceData, st.instanceFinalizer = v, fin
	return nil
}

// InstanceData restituisce il valore impostato con SetInstanceData (nil se assente).
func (e Env) InstanceData() (any, error) {
	st, err := e.state()
	if err != nil {
		return nil, err
	}
	return st.instanceData, nil
}

// handle è un valore Go affidato a Node-API tramite un id (napi_wrap, external, finalizer, cleanup
// hook). onFinalize, se presente, viene chiamata quando Node rilascia l'id.
type handle struct {
	value      any
	onFinalize FinalizeFunc
}

// FinalizeFunc viene chiamata quando Node-API rilascia il valore Go v (oggetto JS raccolto dal GC,
// env distrutto). Gira sul thread JS; con il build tag napi_go_experimental può girare durante il
// GC di V8 e allora non deve chiamare funzioni che eseguono JS.
type FinalizeFunc func(env Env, v any)

var handles table[handle]

func init() {
	sys.SetModuleInitHandler(registerModule)
	sys.SetCallbackHandler(dispatchCallback)
	sys.SetFinalizeHandler(dispatchFinalize)
	sys.SetCleanupHookHandler(dispatchCleanupHook)
}

// dispatchFinalize riceve l'hint (id in handles) di tutti i finalizer di napi.
func dispatchFinalize(env sys.Env, _, hint uintptr) {
	h := handles.remove(hint)
	if h != nil && h.onFinalize != nil {
		h.onFinalize(Env{raw: env}, h.value)
	}
}

// Undefined restituisce undefined.
func (e Env) Undefined() Value {
	var v sys.Value
	// napi_get_undefined fallisce solo con env non valido.
	sys.GetUndefined(e.raw, &v)
	return Value{env: e.raw, raw: v}
}

// Null restituisce null.
func (e Env) Null() Value {
	var v sys.Value
	sys.GetNull(e.raw, &v)
	return Value{env: e.raw, raw: v}
}

// Global restituisce l'oggetto globale.
func (e Env) Global() Object {
	var v sys.Value
	sys.GetGlobal(e.raw, &v)
	return Object{Value{env: e.raw, raw: v}}
}

// RunScript compila ed esegue codice JavaScript (napi_run_script).
func (e Env) RunScript(source string) (Value, error) {
	s, err := e.NewString(source)
	if err != nil {
		return Value{}, err
	}
	var r sys.Value
	st := sys.RunScript(e.raw, s.raw, &r)
	return e.value(r, st)
}

// NapiVersion restituisce la versione Node-API più alta supportata da Node.js.
func (e Env) NapiVersion() (uint32, error) {
	var v uint32
	st := sys.GetVersion(e.raw, &v)
	return v, check(e.raw, st)
}

// NodeVersion è la versione di Node.js (napi_node_version).
type NodeVersion struct {
	Major, Minor, Patch uint32
	Release             string
}

// NodeVersion restituisce la versione di Node.js.
func (e Env) NodeVersion() (NodeVersion, error) {
	var v *sys.NodeVersion
	if err := check(e.raw, sys.GetNodeVersion(e.raw, &v)); err != nil {
		return NodeVersion{}, err
	}
	return NodeVersion{Major: v.Major, Minor: v.Minor, Patch: v.Patch, Release: goStringFromC(v.Release)}, nil
}

// ModuleFileName restituisce l'URL file:// dell'addon (Node-API 9; StatusUnavailable prima).
func (e Env) ModuleFileName() (string, error) {
	var p *byte
	if err := check(e.raw, sys.GetModuleFileName(e.raw, &p)); err != nil {
		return "", err
	}
	return goStringFromC(p), nil
}

// AdjustExternalMemory segnala a V8 memoria esterna tenuta in vita da oggetti JS (positiva se
// allocata, negativa se liberata) e restituisce il totale.
func (e Env) AdjustExternalMemory(delta int64) (int64, error) {
	var total int64
	st := sys.AdjustExternalMemory(e.raw, delta, &total)
	return total, check(e.raw, st)
}

// FatalError termina il processo con un messaggio (napi_fatal_error).
func FatalError(location, message string) {
	sys.FatalError(unsafe.StringData(location), uint(len(location)), unsafe.StringData(message), uint(len(message)))
}

// IsExceptionPending dice se un'eccezione JS è pendente.
func (e Env) IsExceptionPending() bool { return exceptionPending(e.raw) }

// GetAndClearLastException restituisce l'eccezione pendente e la cancella; undefined se non ce
// n'è.
func (e Env) GetAndClearLastException() (Value, error) {
	var v sys.Value
	st := sys.GetAndClearLastException(e.raw, &v)
	return e.value(v, st)
}

// Catch intercetta l'eccezione JS segnalata da err (ErrPendingException): la cancella e la
// restituisce. Per ogni altro err restituisce false e l'eccezione, se c'è, resta pendente.
func (e Env) Catch(err error) (Value, bool) {
	if !errors.Is(err, ErrPendingException) {
		return Value{}, false
	}
	v, gerr := e.GetAndClearLastException()
	if gerr != nil {
		return Value{}, false
	}
	return v, true
}

// Throw lancia err in JS come farebbe una callback che lo restituisce. Se un'eccezione è già
// pendente non fa nulla. Dopo Throw la callback deve tornare senza chiamare altro JS.
func (e Env) Throw(err error) {
	if err != nil {
		throwError(e.raw, err)
	}
}

// ThrowValue lancia v così com'è (napi_throw).
func (e Env) ThrowValue(v Value) error { return check(e.raw, sys.Throw(e.raw, v.raw)) }

// value completa la creazione di un valore: v se st è StatusOK, altrimenti l'errore. Lo status va
// calcolato prima della chiamata (st := sys.X(..., &v); e.value(v, st)): negli argomenti di una
// chiamata Go non garantisce se v viene letto prima o dopo sys.X.
func (e Env) value(v sys.Value, st sys.Status) (Value, error) {
	if st != sys.StatusOK {
		return Value{}, statusError(e.raw, st)
	}
	return Value{env: e.raw, raw: v}, nil
}

// logf scrive su stderr i problemi che non possono arrivare a JS.
func logf(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stderr, format, args...); err != nil {
		// stderr è già l'ultimo canale (si usa quando non si può lanciare in JS): non c'è altro
		// posto dove segnalare che la scrittura è fallita.
		return
	}
}

// Stats conta i valori Go che napi tiene per conto di Node-API, per diagnostica e test di
// perdite di memoria: callback di funzioni e classi vive, handle (valori associati con Wrap,
// external, finalizer, cleanup hook, buffer esterni) e env attivi.
type Stats struct {
	Callbacks int
	Handles   int
	Envs      int
}

// ReadStats restituisce i contatori correnti (sicura da qualsiasi goroutine).
func ReadStats() Stats {
	return Stats{Callbacks: callbacks.len(), Handles: handles.len(), Envs: envStates.len()}
}
