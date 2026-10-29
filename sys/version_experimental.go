//go:build napi_go_experimental && !napi_go_test_version_mismatch

package sys

// #cgo CFLAGS: -DNAPI_VERSION=2147483647 -DNAPI_EXPERIMENTAL
import "C"

// DeclaredVersion è la versione Node-API che il modulo dichiara a Node.js.
// Build tag napi_go_experimental: Node tratta il modulo come sperimentale (ad esempio i finalizer
// possono essere eseguiti durante il GC). node-api-headers non contiene le API sperimentali.
const DeclaredVersion = VersionExperimental
