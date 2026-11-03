/* C → Go trampolines for libuv callbacks used by group B tests (see uv.go). */
#include <stdint.h>
#include "_cgo_export.h"

void gn_test_uv_cb(void* handle) { goNapitestUVCb(handle); }
void gn_test_uv_close_cb(void* handle) { goNapitestUVCloseCb(handle); }
void gn_test_uv_work_cb(void* req) { goNapitestUVWorkCb(req); }
void gn_test_uv_after_work_cb(void* req, int status) { goNapitestUVAfterWorkCb(req, status); }
