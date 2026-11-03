// Command lib is a minimal Go c-shared library for reproducing the musl dlopen issue
// (initial-exec TLS in the Go runtime). Build: CC=musl-gcc go build -buildmode=c-shared -o lib.so .
package main

import "C"

//export GoAnswer
func GoAnswer() C.int { return 42 }

func main() {}
