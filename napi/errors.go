package napi

import (
	"errors"
	"strings"
	"unsafe"

	"github.com/napi-bindings/napi-go/sys"
)

// Status represents a Node-API return status code (napi_status).
type Status = sys.Status

// Common status codes; the complete list is in package sys.
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

// ErrorKind selects the JS error constructor used when throwing an *Error.
type ErrorKind uint8

const (
	KindError       ErrorKind = iota // Error
	KindTypeError                    // TypeError
	KindRangeError                   // RangeError
	KindSyntaxError                  // SyntaxError (Node-API 9)
)

// Error is the error type returned by napi functions.
//
// For a Node-API failure, Status is not StatusOK and Message is the message from
// napi_get_last_error_info, copied immediately (Node overwrites it on the next call). An *Error
// created by addon code (TypeError, RangeError, ...) has Status equal to StatusOK.
//
// When a callback returns an *Error, it is thrown in JS using the Kind constructor, Message as
// message, and Code as code (if non-empty).
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

// JSCode returns the JS error code: Code if set, or for Node-API failures, "ERR_NAPI_" followed
// by the uppercase status name (e.g. ERR_NAPI_INVALID_ARG).
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

// Is makes ErrPendingException match via errors.Is even for copies of the error.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t == ErrPendingException && e.Status == StatusPendingException
}

// ErrPendingException indicates that a JS exception is pending (thrown by JS code called from Go,
// or by an earlier throw). Returning it from a callback lets the exception propagate as-is;
// Env.Catch intercepts it.
var ErrPendingException = &Error{Status: StatusPendingException, Message: "a JavaScript exception is pending"}

// TypeError, RangeError, and SyntaxError create errors that are thrown in JS using the
// corresponding error constructor. code may be empty.
func TypeError(code, message string) *Error {
	return &Error{Kind: KindTypeError, Code: code, Message: message}
}

// RangeError creates a JS RangeError; see TypeError.
func RangeError(code, message string) *Error {
	return &Error{Kind: KindRangeError, Code: code, Message: message}
}

// SyntaxError creates a JS SyntaxError; see TypeError. Without Node-API 9, it is thrown as a plain Error.
func SyntaxError(code, message string) *Error {
	return &Error{Kind: KindSyntaxError, Code: code, Message: message}
}

// Coder is implemented by errors that specify a JS error code.
type Coder interface{ JSCode() string }

// Thrown is an error that, when returned from a callback, throws its Value in JS as-is (even if
// the value is not an Error). It is valid only within the callback where the value was created;
// returned from WithScope or WithEscapableScope, it is thrown before the scope closes (the
// functions then return ErrPendingException).
type Thrown struct{ Value Value }

func (t *Thrown) Error() string { return "napi: JavaScript value thrown" }

// Throw returns an error that throws v in JS (see Thrown).
func Throw(v Value) error { return &Thrown{Value: v} }

// Check converts a status returned by a sys function into the error napi would return for it (nil
// for StatusOK, ErrPendingException, or an *Error carrying the message from
// napi_get_last_error_info). It is meant for addons that call sys directly for APIs not wrapped by
// napi; call it right after the sys call, before any other Node-API call overwrites the message.
func (e Env) Check(st Status) error { return check(e.raw, st) }

// check converts a status into an error. The StatusOK fast path does not allocate or cross cgo.
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
	// StatusUnavailable originates from napi-go: Node has no extended error info to add.
	if st != sys.StatusUnavailable && sys.GetLastErrorInfo(env, &info) == sys.StatusOK && info != nil &&
		info.ErrorCode == st && info.ErrorMessage != nil {
		msg = goStringFromC(info.ErrorMessage)
	}
	if msg == "" {
		msg = st.String()
	}
	return &Error{Status: st, Kind: statusKind(st), Message: msg}
}

// statusKind maps "wrong value type" statuses to TypeError in JS, and all other statuses to Error.
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

// goStringFromC copies a NUL-terminated C string into a Go string.
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

// throwError throws err in JS according to the Error rules, unless an exception is already
// pending (in which case the existing exception is preserved and allowed to propagate).
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
		st := sys.StatusGenericFailure
		if ne, ok := cerr.(*Error); ok {
			st = ne.Status
		}
		reportThrowFailure(st, err)
		return
	}
	reportThrowFailure(sys.Throw(env, v), err)
}

func exceptionPending(env sys.Env) bool {
	var pending bool
	return sys.IsExceptionPending(env, &pending) == sys.StatusOK && pending
}

// newErrorValue creates the JS Error object representing err.
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

// reportThrowFailure logs to stderr if throwing the exception itself fails, matching sys behavior.
// When the env can no longer run JavaScript (a worker or the process is shutting down) Node-API
// fails with napi_pending_exception or napi_cannot_run_js while no exception is pending: nobody
// can receive the error, so it is dropped silently.
func reportThrowFailure(st sys.Status, err error) {
	if st != sys.StatusOK && st != sys.StatusPendingException && st != sys.StatusCannotRunJS {
		logf("napi-go: cannot throw %q (%s)\n", err.Error(), st)
	}
}

// NewError creates the JS Error object representing err, following the same rules as a callback
// returning err (Kind and Code from *Error, JSCode from Coder).
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
