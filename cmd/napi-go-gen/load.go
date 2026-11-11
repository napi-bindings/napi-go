package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// napiPath is the import path of the napi package.
const napiPath = "github.com/napi-bindings/napi-go/napi"

// loaded is a parsed and type-checked package.
type loaded struct {
	fset  *token.FileSet
	files []*ast.File
	pkg   *types.Package
	info  *types.Info
	napi  *types.Package
}

// listed is the part of the output of go list -json used here.
type listed struct {
	ImportPath string
	Dir        string
	Name       string
	GoFiles    []string
	CgoFiles   []string
	Export     string
	DepOnly    bool
	Error      *struct{ Err string }
}

// load parses and type-checks the package in dir, without the generated file skip (it may refer
// to declarations that no longer exist). Dependencies are read from the export data of the Go
// build cache (go list -export), so they are compiled like in a normal build, cgo included.
func load(dir, tags, skip string) (*loaded, error) {
	args := []string{"list", "-e", "-json=ImportPath,Dir,Name,GoFiles,CgoFiles,Export,DepOnly,Error", "-deps", "-export"}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "--", ".", napiPath)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	exports := map[string]string{}
	var target *listed
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var l listed
		if err := dec.Decode(&l); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list: %w", err)
		}
		if l.Export != "" {
			exports[l.ImportPath] = l.Export
		}
		if d, err := filepath.EvalSymlinks(l.Dir); err == nil && d == realDir {
			target = &l
			continue // its own build errors are reported by the type checker below
		}
		if l.Error != nil {
			return nil, fmt.Errorf("%s: %s", l.ImportPath, l.Error.Err)
		}
	}
	if target == nil {
		return nil, fmt.Errorf("no Go package in %s", dir)
	}
	if exports[napiPath] == "" {
		return nil, fmt.Errorf("package %s is not available: add it to go.mod (go get %s)", napiPath, napiPath)
	}

	p := &loaded{fset: token.NewFileSet()}
	for _, name := range append(target.GoFiles, target.CgoFiles...) {
		if name == skip {
			continue
		}
		f, err := parser.ParseFile(p.fset, filepath.Join(target.Dir, name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		p.files = append(p.files, f)
	}
	if len(p.files) == 0 {
		return nil, fmt.Errorf("no Go files in %s", dir)
	}
	imp := importer.ForCompiler(p.fset, "gc", func(path string) (io.ReadCloser, error) {
		file := exports[path]
		if file == "" {
			return nil, fmt.Errorf("no export data for %q", path)
		}
		return os.Open(file)
	})
	var typeErrs []string
	conf := types.Config{
		Importer:    imp,
		FakeImportC: true,
		Error: func(err error) {
			if len(typeErrs) < maxTypeErrors {
				typeErrs = append(typeErrs, err.Error())
			}
		},
	}
	p.info = &types.Info{Defs: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	// The returned error is the first of those already collected by conf.Error.
	p.pkg, _ = conf.Check(target.ImportPath, p.fset, p.files, p.info)
	if len(typeErrs) > 0 {
		return nil, fmt.Errorf("the package does not compile:\n%s", strings.Join(typeErrs, "\n"))
	}
	if p.napi, err = imp.Import(napiPath); err != nil {
		return nil, err
	}
	return p, nil
}

// maxTypeErrors bounds the type errors reported for a package that does not compile.
const maxTypeErrors = 10
