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

// Equivalents of the "basic" macros in common.h, used in finalizers that run during GC
// (node_api_basic_env): a JS exception cannot be thrown there, so a failure terminates the
// process with abort(), just as in C. Errors from writing to stderr before abort() are
// intentionally ignored: the process terminates immediately afterward and there is no other
// channel to report them to (matching fprintf in C).

// Abort corresponds to abort(): terminates the process with SIGABRT (exit code 134 on Windows).
func Abort() {
	C.gn_test_abort()
}

// BasicCall corresponds to NODE_API_BASIC_CALL_BASE: if status is not StatusOK, writes the last
// error message to stderr (FATALLY_FAIL_WITH_LAST_ERROR) and calls abort().
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

// BasicAssert corresponds to NODE_API_BASIC_ASSERT_BASE: if cond is false, writes
// "assertion (expr) failed: message" to stderr and calls abort(). expr is the C condition text.
func BasicAssert(cond bool, expr, message string) bool {
	if cond {
		return true
	}
	fmt.Fprint(os.Stderr, "assertion ("+expr+") failed: "+message)
	Abort()
	return false
}

// CAssert corresponds to assert(expr) from <assert.h> in a build with assertions enabled.
func CAssert(cond bool, expr string) {
	if !cond {
		fmt.Fprintln(os.Stderr, "Assertion failed: "+expr)
		Abort()
	}
}
