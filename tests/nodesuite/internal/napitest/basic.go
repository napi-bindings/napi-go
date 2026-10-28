package napitest

/*
#include <stdlib.h>

static void gn_test_abort(void) { abort(); }
*/
import "C"

import (
	"fmt"
	"os"

	"github.com/napi-bindings/napi-go/sys"
)

// Equivalenti delle macro "basic" di common.h, usate nei finalizer che girano durante il GC
// (node_api_basic_env): lì non si può lanciare un'eccezione JS, quindi un fallimento termina il
// processo con abort(), come nel C. L'errore delle scritture su stderr che precedono abort() non
// è controllato di proposito: il processo termina subito dopo e non c'è altro canale a cui
// riportarlo (come il fprintf del C).

// Abort equivale ad abort(): termina il processo con SIGABRT (exit code 134 su Windows).
func Abort() {
	C.gn_test_abort()
}

// BasicCall equivale a NODE_API_BASIC_CALL_BASE: se status non è StatusOK scrive su stderr il
// messaggio dell'ultimo errore (FATALLY_FAIL_WITH_LAST_ERROR) e chiama abort().
func BasicCall(env sys.Env, status sys.Status) bool {
	if status == sys.StatusOK {
		return true
	}
	message := emptyErrorMessage
	var info *sys.ExtendedErrorInfo
	if status == sys.StatusUnavailable {
		message = unavailableMessage
	} else if sys.GetLastErrorInfo(env, &info) == sys.StatusOK && info != nil && info.ErrorMessage != nil {
		message = GoString(info.ErrorMessage)
	}
	fmt.Fprintln(os.Stderr, message)
	Abort()
	return false
}

// BasicAssert equivale a NODE_API_BASIC_ASSERT_BASE: se cond è falsa scrive su stderr
// "assertion (expr) failed: message" e chiama abort(). expr è il testo C della condizione.
func BasicAssert(cond bool, expr, message string) bool {
	if cond {
		return true
	}
	fmt.Fprint(os.Stderr, "assertion ("+expr+") failed: "+message)
	Abort()
	return false
}

// CAssert equivale ad assert(expr) di <assert.h> in una build con le asserzioni attive.
func CAssert(cond bool, expr string) {
	if !cond {
		fmt.Fprintln(os.Stderr, "Assertion failed: "+expr)
		Abort()
	}
}
