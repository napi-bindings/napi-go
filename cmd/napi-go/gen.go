package main

// gen.go — napi-go gen: the code generator (internal/codegen).

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/napi-bindings/napi-go/internal/codegen"
)

func runGen(args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet(stderr, "gen", "")
	dir := fs.String("C", ".", "directory of the project (package.json) or of the Go package")
	check := fs.Bool("check", false, "do not write: fail if the generated files are out of date")
	name := fs.String("name", "", "base name of the .node files (default: binaryName of package.json, or the directory name)")
	pkgName := fs.String("package-name", "", "npm package whose platform packages the entry point loads (default: name of package.json)")
	js := fs.String("js", "", "generated JS entry point, relative to the project; empty: none (default index.js)")
	dts := fs.String("dts", "", "generated TypeScript declarations, relative to the project; empty: none (default index.d.ts)")
	tags := fs.String("tags", "", "Go build tags (default: tags of package.json)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		fs.Usage()
		return errUsage
	}
	p, err := findProject(*dir)
	if err != nil {
		return err
	}
	cfg := p.codegenConfig()
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["name"] {
		if !validBinaryName(*name) {
			return fmt.Errorf("-name %q: use letters, digits, '-', '_' and '.' only", *name)
		}
		cfg.BinaryName = *name
	}
	if set["package-name"] {
		cfg.PackageName = *pkgName
	}
	if set["js"] {
		cfg.JSFile = projectFile(p, *js)
	}
	if set["dts"] {
		cfg.DTSFile = projectFile(p, *dts)
	}
	if set["tags"] {
		cfg.Tags = *tags
	}
	return generate(cfg, *check, stdout)
}

// projectFile resolves a file name given on the command line; empty stays empty (no file).
func projectFile(p *project, name string) string {
	if name == "" {
		return ""
	}
	return p.path(name)
}

// generate runs the generator, writing the files or, with check, comparing them with the files on
// disk.
func generate(cfg codegen.Config, check bool, stdout io.Writer) error {
	files, err := codegen.Generate(cfg)
	if err != nil {
		return err
	}
	if check {
		return codegen.Check(files)
	}
	if err := codegen.Write(files); err != nil {
		return err
	}
	for _, f := range files {
		rel, err := filepath.Rel(cfg.Dir, f.Path)
		if err != nil {
			rel = f.Path
		}
		if _, err := fmt.Fprintf(stdout, "generated %s\n", filepath.ToSlash(rel)); err != nil {
			return err
		}
	}
	return nil
}

// findProject returns the project of dir: the one whose package.json is in dir, or else the
// nearest package.json above dir with a napiGo configuration whose Go package is dir (gen also
// runs from go generate, in the directory of the Go package), or else the bare Go package in dir.
func findProject(dir string) (*project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if here, err := fileExists(filepath.Join(abs, packageJSONFile)); err != nil || here {
		if err != nil {
			return nil, err
		}
		return loadProject(abs)
	}
	for cur := filepath.Dir(abs); ; {
		path := filepath.Join(cur, packageJSONFile)
		ok, err := fileExists(path)
		if err != nil {
			return nil, err
		}
		if ok {
			pkg, err := readJSONObject(path)
			if err != nil {
				return nil, err
			}
			if pkg.has(configKey) {
				p, err := loadProject(cur)
				if err != nil {
					return nil, err
				}
				if p.goPackageDir() == abs {
					return p, nil
				}
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return loadProject(abs)
}
