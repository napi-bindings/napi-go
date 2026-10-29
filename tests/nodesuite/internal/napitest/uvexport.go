package napitest

/*
#include <stdint.h>
*/
import "C"

import "unsafe"

// Ingressi Go dei trampolini di uv.c. I puntatori sono memoria C (handle e richieste di libuv).

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
