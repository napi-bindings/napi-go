// Command lib è un c-shared Go minimo per riprodurre il problema di dlopen con musl
// (TLS initial-exec del runtime Go). Build: CC=musl-gcc go build -buildmode=c-shared -o lib.so .
package main

import "C"

//export GoAnswer
func GoAnswer() C.int { return 42 }

func main() {}
