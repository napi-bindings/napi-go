/* Trampolino C → Go per node_api_noenv_finalize (vedi noenv.go e experimental.go). */
#include <stdint.h>
#include "_cgo_export.h"

void gn_test_noenv_finalize(void* data, void* hint) {
  goNapitestNoEnvFinalize((uintptr_t)data, (uintptr_t)hint);
}
