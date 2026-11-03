// Command gen generates Layer 0 of napi-go (package sys) from the vendored Node-API headers.
//
// Usage, from the sys directory:
//
//	go run ./internal/gen          # regenerate zz_* files
//	go run ./internal/gen -check   # verify that zz_* files are up to date (CI)
//
// It reads include/*.h (function prototypes, enums) and include/symbols.js (minimum version for
// each function) and emits:
//
//	zz_api.h, zz_api.c             function pointer table and static inline C wrappers
//	zz_api.go                      1:1 Go wrappers, Fn enum, noescape directives
//	zz_enums.go                    Node-API enums as Go types
//	zz_nocallback*.go              nocallback directives, per build tag
//
// Curated decisions that cannot be inferred from the headers live in policy.go.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

var headerFiles = []string{"js_native_api.h", "node_api.h"}

func main() {
	dir := flag.String("dir", ".", "path to the sys package directory")
	check := flag.Bool("check", false, "do not write files; exit with an error if generated files are out of date")
	flag.Parse()

	api, err := load(filepath.Join(*dir, "include"))
	if err != nil {
		fail(err)
	}
	files, err := emitAll(api)
	if err != nil {
		fail(err)
	}
	stale := 0
	for _, f := range files {
		path := filepath.Join(*dir, f.Path)
		if *check {
			cur, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(cur, f.Content) {
				fmt.Fprintf(os.Stderr, "gen: %s is out of date: run go generate ./sys\n", path)
				stale++
			}
			continue
		}
		if err := os.WriteFile(path, f.Content, 0o644); err != nil {
			fail(err)
		}
	}
	if stale > 0 {
		os.Exit(1)
	}
	if !*check {
		fmt.Printf("gen: %d functions (Node-API 1–%d), %d enums, node-api-headers %s\n",
			len(api.Funcs), api.MaxVersion, len(api.Enums), headersVersion)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}

// load reads the headers and symbols.js and constructs the complete API model.
func load(includeDir string) (API, error) {
	var funcs []Func
	var enums []Enum
	for _, name := range append([]string{"js_native_api_types.h", "node_api_types.h"}, headerFiles...) {
		src, err := os.ReadFile(filepath.Join(includeDir, name))
		if err != nil {
			return API{}, err
		}
		e, err := parseEnums(string(src))
		if err != nil {
			return API{}, fmt.Errorf("%s: %w", name, err)
		}
		enums = append(enums, e...)
	}
	for _, name := range headerFiles {
		src, err := os.ReadFile(filepath.Join(includeDir, name))
		if err != nil {
			return API{}, err
		}
		f, err := parseFuncs(name, string(src))
		if err != nil {
			return API{}, err
		}
		funcs = append(funcs, f...)
	}
	symSrc, err := os.ReadFile(filepath.Join(includeDir, "symbols.js"))
	if err != nil {
		return API{}, err
	}
	versions, maxVersion, err := parseSymbols(string(symSrc))
	if err != nil {
		return API{}, err
	}
	return build(funcs, enums, versions, maxVersion)
}

// build verifies consistency across headers, symbols.js, and policy, and binds Go types.
func build(funcs []Func, enums []Enum, versions map[string]int, maxVersion int) (API, error) {
	if err := assignVersions(funcs, versions); err != nil {
		return API{}, err
	}
	if err := validatePolicy(funcs, enums); err != nil {
		return API{}, err
	}
	api := API{Enums: enums, MaxVersion: maxVersion}
	goNames := map[string]string{}
	for _, f := range funcs {
		if _, skip := excluded[f.Name]; skip {
			continue
		}
		fb, err := bindFunc(f)
		if err != nil {
			return API{}, err
		}
		if other, dup := goNames[fb.GoName]; dup {
			return API{}, fmt.Errorf("%s and %s map to the same Go name %s", other, f.Name, fb.GoName)
		}
		goNames[fb.GoName] = f.Name
		api.Funcs = append(api.Funcs, fb)
	}
	enumNames := map[string]string{}
	for _, e := range enums {
		for _, m := range e.Members {
			g := goEnumName(m.CName, enumConfig[e.CName])
			if other, dup := enumNames[g]; dup {
				return API{}, fmt.Errorf("%s and %s map to the same Go name %s", other, m.CName, g)
			}
			enumNames[g] = m.CName
		}
	}
	return api, nil
}
