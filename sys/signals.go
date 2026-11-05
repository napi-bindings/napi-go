package sys

/*
#include "internal.h"
*/
import "C"

// RepairNodeSignalHandlers restores SA_RESETHAND on Node's SIGINT and SIGTERM handlers
// (node::SignalExit) when it is missing. The module entry point already does this right after the
// Go runtime starts (sigfix.c): on macOS the runtime reinstalls existing handlers without
// SA_RESETHAND, and without the repair SIGINT/SIGTERM with no JS listener would keep the process
// spinning instead of terminating it.
//
// Calling it again is idempotent and harmless; it exists for tests and diagnostics, which can
// reproduce the loss of SA_RESETHAND on any Unix and check the repair. It does nothing on Windows
// or when the handlers are not Node's.
func RepairNodeSignalHandlers() { C.gn_repair_node_signal_handlers() }
