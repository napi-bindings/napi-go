package napi

import (
	"errors"
	"strings"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Status è il codice di ritorno di Node-API (napi_status).
type Status = sys.Status

// Status principali; l'elenco completo è in sys.
const (
	StatusOK               = sys.StatusOK
	StatusInvalidArg       = sys.StatusInvalidArg
	StatusObjectExpected   = sys.StatusObjectExpected
	StatusStringExpected   = sys.StatusStringExpected
	StatusFunctionExpected = sys.StatusFunctionExpected
	StatusNumberExpected   = sys.StatusNumberExpected
	StatusBooleanExpected  = sys.StatusBooleanExpected
	StatusArrayExpected    = sys.StatusArrayExpected
	StatusGenericFailure   = sys.StatusGenericFailure
	StatusPendingException = sys.StatusPendingException
	StatusBigIntExpected   = sys.StatusBigIntExpected
	StatusDateExpected     = sys.StatusDateExpected
	StatusUnavailable      = sys.StatusUnavailable
)

// ErrorKind sceglie il costruttore JS con cui un *Error viene lanciato.
type ErrorKind uint8

const (
	KindError       ErrorKind = iota // Error
	KindTypeError                    // TypeError
	KindRangeError                   // RangeError
	KindSyntaxError                  // SyntaxError (Node-API 9)
)

// Error è l'errore restituito dalle funzioni di napi.
//
// Per un fallimento di Node-API Status è diverso da StatusOK e Message è il messaggio di
// napi_get_last_error_info, copiato subito (Node lo sovrascrive alla chiamata successiva). Un
// *Error creato dal codice dell'addon (TypeError, RangeError, ...) ha Status StatusOK.
//
// Quando una callback restituisce un *Error, viene lanciato in JS con il costruttore di Kind,
// Message come message e Code come code (se non vuoto).
type Error struct {
	Status  Status
	Kind    ErrorKind
	Code    string
	Message string
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("napi: ")
	if e.Code != "" {
		b.WriteString(e.Code)
		b.WriteString(": ")
	}
	b.WriteString(e.Message)
	if e.Status != StatusOK && e.Code == "" {
		b.WriteString(" (")
		b.WriteString(e.Status.String())
		b.WriteString(")")
	}
	return b.String()
}

// JSCode restituisce il code dell'errore JS: Code se impostato, altrimenti, per i fallimenti di
// Node-API, "ERR_NAPI_" seguito dal nome dello status (es. ERR_NAPI_INVALID_ARG).
func (e *Error) JSCode() string {
	if e.Code != "" || e.Status == StatusOK {
		return e.Code
	}
	name := strings.TrimPrefix(e.Status.String(), "napi_")
	if e.Status == StatusUnavailable {
		name = "unavailable"
	}
	return "ERR_NAPI_" + strings.ToUpper(name)
}

// Is rende ErrPendingException confrontabile con errors.Is anche per copie dell'errore.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t == ErrPendingException && e.Status == StatusPendingException
}

// ErrPendingException indica che un'eccezione JS è pendente (lanciata da codice JS chiamato da Go,
// o da un throw precedente). Una callback che la restituisce lascia propagare l'eccezione così
// com'è; Env.Catch la intercetta.
var ErrPendingException = &Error{Status: StatusPendingException, Message: "a JavaScript exception is pending"}

// TypeError, RangeError e SyntaxError creano errori lanciati in JS con il costruttore
// corrispondente. code può essere vuoto.
func TypeError(code, message string) *Error {
	return &Error{Kind: KindTypeError, Code: code, Message: message}
}

// RangeError: vedi TypeError.
func RangeError(code, message string) *Error {
	return &Error{Kind: KindRangeError, Code: code, Message: message}
}

// SyntaxError: vedi TypeError. Senza Node-API 9 viene lanciato come Error.
func SyntaxError(code, message string) *Error {
	return &Error{Kind: KindSyntaxError, Code: code, Message: message}
}

// Coder è implementata dagli errori che impostano il code dell'errore JS.
type Coder interface{ JSCode() string }

// Thrown è un errore che, restituito da una callback, lancia in JS il valore così com'è (anche
// un valore che non è un Error). Vale solo nella callback in cui il valore è stato creato.
type Thrown struct{ Value Value }

func (t *Thrown) Error() string { return "napi: JavaScript value thrown" }

// Throw restituisce un errore che lancia v in JS (vedi Thrown).
func Throw(v Value) error { return &Thrown{Value: v} }

// check converte uno status in error. Il percorso con StatusOK non alloca e non attraversa cgo.
func check(env sys.Env, st sys.Status) error {
	if st == sys.StatusOK {
		return nil
	}
	return statusError(env, st)
}

func statusError(env sys.Env, st sys.Status) error {
	if st == sys.StatusPendingException {
		return ErrPendingException
	}
	msg := ""
	var info *sys.ExtendedErrorInfo
	// StatusUnavailable è di napi-go: Node non ha informazioni da aggiungere.
	if st != sys.StatusUnavailable && sys.GetLastErrorInfo(env, &info) == sys.StatusOK && info != nil &&
		info.ErrorCode == st && info.ErrorMessage != nil {
		msg = goStringFromC(info.ErrorMessage)
	}
	if msg == "" {
		msg = st.String()
	}
	return &Error{Status: st, Kind: statusKind(st), Message: msg}
}

// statusKind: gli status "valore del tipo sbagliato" diventano TypeError in JS, gli altri Error.
func statusKind(st sys.Status) ErrorKind {
	switch st {
	case sys.StatusObjectExpected, sys.StatusStringExpected, sys.StatusNameExpected,
		sys.StatusFunctionExpected, sys.StatusNumberExpected, sys.StatusBooleanExpected,
		sys.StatusArrayExpected, sys.StatusBigIntExpected, sys.StatusDateExpected,
		sys.StatusArrayBufferExpected, sys.StatusDetachableArrayBufferExpected:
		return KindTypeError
	}
	return KindError
}

// goStringFromC copia una stringa C terminata da NUL.
func goStringFromC(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	return string(unsafe.Slice(p, n))
}

// throwError lancia err in JS secondo le regole di Error, a meno che un'eccezione non sia già
// pendente (quella resta e si propaga).
func throwError(env sys.Env, err error) {
	if errors.Is(err, ErrPendingException) || exceptionPending(env) {
		return
	}
	var thrown *Thrown
	if errors.As(err, &thrown) && thrown.Value.raw != nil {
		reportThrowFailure(sys.Throw(env, thrown.Value.raw), err)
		return
	}
	v, cerr := newErrorValue(env, err)
	if cerr != nil {
		reportThrowFailure(sys.StatusGenericFailure, err)
		return
	}
	reportThrowFailure(sys.Throw(env, v), err)
}

func exceptionPending(env sys.Env) bool {
	var pending bool
	return sys.IsExceptionPending(env, &pending) == sys.StatusOK && pending
}

// newErrorValue crea l'oggetto Error JS che rappresenta err.
func newErrorValue(env sys.Env, err error) (sys.Value, error) {
	kind, code, msg := KindError, "", err.Error()
	var ne *Error
	var coder Coder
	switch {
	case errors.As(err, &ne):
		kind, code, msg = ne.Kind, ne.JSCode(), ne.Message
	case errors.As(err, &coder):
		code = coder.JSCode()
	}
	return createError(env, kind, code, msg)
}

func createError(env sys.Env, kind ErrorKind, code, msg string) (sys.Value, error) {
	var codeV, msgV, result sys.Value
	if code != "" {
		if err := check(env, sys.CreateStringUTF8(env, unsafe.StringData(code), uint(len(code)), &codeV)); err != nil {
			return nil, err
		}
	}
	if err := check(env, sys.CreateStringUTF8(env, unsafe.StringData(msg), uint(len(msg)), &msgV)); err != nil {
		return nil, err
	}
	var st sys.Status
	switch kind {
	case KindTypeError:
		st = sys.CreateTypeError(env, codeV, msgV, &result)
	case KindRangeError:
		st = sys.CreateRangeError(env, codeV, msgV, &result)
	case KindSyntaxError:
		st = sys.CreateSyntaxError(env, codeV, msgV, &result)
		if st == sys.StatusUnavailable {
			st = sys.CreateError(env, codeV, msgV, &result)
		}
	default:
		st = sys.CreateError(env, codeV, msgV, &result)
	}
	return result, check(env, st)
}

// reportThrowFailure: se nemmeno il lancio riesce, l'errore resta su stderr come in sys.
func reportThrowFailure(st sys.Status, err error) {
	if st != sys.StatusOK {
		logf("napi-go: cannot throw %q (%s)\n", err.Error(), st)
	}
}

// NewError crea l'oggetto Error JS che rappresenta err, con le stesse regole di una callback che
// lo restituisce (Kind e Code di *Error, JSCode di Coder).
func (e Env) NewError(err error) (Object, error) {
	if err == nil {
		return Object{}, TypeError("", "NewError: nil error")
	}
	v, cerr := newErrorValue(e.raw, err)
	if cerr != nil {
		return Object{}, cerr
	}
	return Object{Value{env: e.raw, raw: v}}, nil
}
