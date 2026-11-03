package napitest

/*
#include <stdint.h>
*/
import "C"

import "unsafe"

// Go entry points for the trampolines in uv.c. Pointers refer to C memory (libuv handles and requests).

//export goNapitestUVCb
func goNapitestUVCb(handle unsafe.Pointer) { dispatchUVCb(handle) }

//export goNapitestUVCloseCb
func goNapitestUVCloseCb(handle unsafe.Pointer) { dispatchUVCloseCb(handle) }

//export goNapitestUVWorkCb
func goNapitestUVWorkCb(req unsafe.Pointer) { dispatchUVWorkCb(req) }

//export goNapitestUVAfterWorkCb
func goNapitestUVAfterWorkCb(req unsafe.Pointer, status C.int) {
	dispatchUVAfterWorkCb(req, int(status))
}
