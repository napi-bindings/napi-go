// Comando gen genera il Layer 0 di napi-go (pacchetto sys) dagli header Node-API vendorizzati.
//
// Uso, dalla directory sys:
//
//	go run ./internal/gen          # rigenera i file zz_*
//	go run ./internal/gen -check   # verifica che i file zz_* siano aggiornati (CI)
//
// Legge include/*.h (prototipi, enum) e include/symbols.js (versione minima di ogni funzione) e
// produce:
//
//	zz_api.h, zz_api.c             tabella dei puntatori e wrapper C static inline
//	zz_api.go                      wrapper Go 1:1, enum Fn, direttive noescape
//	zz_enums.go                    enum Node-API come tipi Go
//	zz_nocallback*.go              direttive nocallback, per build tag
//
// Le decisioni non deducibili dagli header sono in policy.go.
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
	dir := flag.String("dir", ".", "directory del pacchetto sys")
	check := flag.Bool("check", false, "non scrive: esce con errore se i file generati non sono aggiornati")
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
				fmt.Fprintf(os.Stderr, "gen: %s non è aggiornato: eseguire go generate ./sys\n", path)
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
		fmt.Printf("gen: %d funzioni (Node-API 1–%d), %d enum, node-api-headers %s\n",
			len(api.Funcs), api.MaxVersion, len(api.Enums), headersVersion)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}

// load legge gli header e symbols.js e costruisce il modello completo.
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

// build verifica la coerenza tra header, symbols.js e policy e lega i tipi Go.
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
			return API{}, fmt.Errorf("%s e %s hanno lo stesso nome Go %s", other, f.Name, fb.GoName)
		}
		goNames[fb.GoName] = f.Name
		api.Funcs = append(api.Funcs, fb)
	}
	enumNames := map[string]string{}
	for _, e := range enums {
		for _, m := range e.Members {
			g := goEnumName(m.CName, enumConfig[e.CName])
			if other, dup := enumNames[g]; dup {
				return API{}, fmt.Errorf("%s e %s hanno lo stesso nome Go %s", other, m.CName, g)
			}
			enumNames[g] = m.CName
		}
	}
	return api, nil
}
