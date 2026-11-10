package main

// model.go — reads the annotations of a package and builds the model of its exports.

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// maxArity is the largest number of parameters of the napi wrappers (Func0..Func8).
const maxArity = 8

// directivePrefix starts every annotation, written like a Go directive (no space after //).
const directivePrefix = "//napi:"

// module is everything the generator emits for a package.
type module struct {
	pkgName string
	funcs   []*funcModel
	classes []*classModel
	consts  []*constModel
	objects []*objectModel
}

// funcModel is an exported function, a method, a static method or a constructor.
type funcModel struct {
	pos     token.Pos
	jsName  string
	goName  string
	doc     string
	wrapper string // napi wrapper, e.g. "FuncE2"
	params  []paramModel
	rest    bool   // takes a napi.CallContext: any further JS arguments
	ret     string // TypeScript return type
}

type paramModel struct{ name, ts string }

// accessorModel is a class property backed by a getter and an optional setter.
type accessorModel struct {
	pos            token.Pos
	jsName, doc    string
	getter, setter string // Go method names; setter may be empty
	ts             string
	fallible       bool // the methods return an error: napi.AccessorE
	valueType      types.Type
}

type classModel struct {
	pos       token.Pos
	jsName    string
	goName    string
	doc       string
	ctor      *funcModel
	methods   []*funcModel
	accessors []*accessorModel
	statics   []*funcModel
	consts    []*constModel
	members   map[string]token.Pos // JS names in use ("static x" for statics)
}

type constModel struct {
	pos                     token.Pos
	jsName, goName, doc, ts string
}

// objectModel is a struct emitted as a TypeScript interface.
type objectModel struct {
	pos    token.Pos
	name   string
	doc    string
	fields []fieldModel
}

type fieldModel struct {
	name, ts, doc string
	optional      bool
}

// directive is one //napi: annotation.
type directive struct {
	name, arg string
	pos       token.Pos
}

// analyzer builds a module from a loaded package.
type analyzer struct {
	*loaded
	mod       *module
	fromJS    *types.Interface
	toJS      *types.Interface
	classes   map[*types.TypeName]*classModel
	objects   map[*types.TypeName]*objectModel
	objNames  map[*types.TypeName]string // //napi:object renames
	overrides map[*types.TypeName]string // //napi:type
	typeDocs  map[*types.TypeName]string
	fieldDocs map[*types.Var]string
	names     map[string]token.Pos // module-level JS names
	tsNames   map[string]token.Pos // TypeScript type names (classes and interfaces)
	errs      []string
}

func analyze(p *loaded) (*module, error) {
	g := &analyzer{
		loaded:    p,
		mod:       &module{pkgName: p.pkg.Name()},
		classes:   map[*types.TypeName]*classModel{},
		objects:   map[*types.TypeName]*objectModel{},
		objNames:  map[*types.TypeName]string{},
		overrides: map[*types.TypeName]string{},
		typeDocs:  map[*types.TypeName]string{},
		fieldDocs: map[*types.Var]string{},
		names:     map[string]token.Pos{},
		tsNames:   map[string]token.Pos{},
	}
	for _, name := range []string{"FromJS", "ToJS"} {
		var iface *types.Interface
		if obj := p.napi.Scope().Lookup(name); obj != nil {
			iface, _ = obj.Type().Underlying().(*types.Interface)
		}
		if iface == nil {
			return nil, fmt.Errorf("%s.%s is not an interface: napi-go-gen does not match this version of napi", napiPath, name)
		}
		if name == "FromJS" {
			g.fromJS = iface
		} else {
			g.toJS = iface
		}
	}
	g.collectTypes()
	g.collectValues()
	g.collectFuncs()
	g.finish()
	if len(g.errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(g.errs, "\n"))
	}
	return g.mod, nil
}

func (g *analyzer) errorf(pos token.Pos, format string, args ...any) {
	p := g.fset.Position(pos)
	g.errs = append(g.errs, fmt.Sprintf("%s:%d: %s", p.Filename, p.Line, fmt.Sprintf(format, args...)))
}

// directives returns the //napi: annotations of a doc comment.
func directives(cg *ast.CommentGroup) []directive {
	if cg == nil {
		return nil
	}
	var out []directive
	for _, c := range cg.List {
		rest, ok := strings.CutPrefix(c.Text, directivePrefix)
		if !ok {
			continue
		}
		name, arg, _ := strings.Cut(rest, " ")
		out = append(out, directive{name: name, arg: strings.TrimSpace(arg), pos: c.Pos()})
	}
	return out
}

// allowedDirectives reports the directives of ds that are not in allowed and returns the others
// by name.
func (g *analyzer) allowedDirectives(ds []directive, where string, allowed ...string) map[string]directive {
	out := map[string]directive{}
	for _, d := range ds {
		ok := false
		for _, a := range allowed {
			ok = ok || d.name == a
		}
		if !ok {
			g.errorf(d.pos, "//napi:%s cannot be used on %s (allowed here: %s)", d.name, where, strings.Join(allowed, ", "))
			continue
		}
		if _, dup := out[d.name]; dup {
			g.errorf(d.pos, "//napi:%s is repeated", d.name)
		}
		out[d.name] = d
	}
	return out
}

func docText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	return strings.TrimSpace(cg.Text()) // Text drops the directive lines
}

// claim records the JS name of a module-level export, reporting duplicates.
func (g *analyzer) claim(names map[string]token.Pos, kind, name string, pos token.Pos) {
	if !validJSName(name, strings.Contains(kind, "member")) {
		g.errorf(pos, "%q is not a valid JS name for %s", name, kind)
		return
	}
	if prev, dup := names[name]; dup {
		g.errorf(pos, "duplicate %s %q (also at line %d)", kind, name, g.fset.Position(prev).Line)
		return
	}
	names[name] = pos
}

// ---- types: classes, objects, overrides ----

func (g *analyzer) collectTypes() {
	for _, f := range g.files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				doc := ts.Doc
				if doc == nil && !gd.Lparen.IsValid() {
					doc = gd.Doc
				}
				obj, _ := g.info.Defs[ts.Name].(*types.TypeName)
				if obj == nil {
					continue
				}
				g.typeDocs[obj] = docText(doc)
				if st, ok := ts.Type.(*ast.StructType); ok {
					g.collectFieldDocs(st)
				}
				ds := g.allowedDirectives(directives(doc), "a type", "class", "object", "type")
				if len(ds) > 1 {
					g.errorf(ts.Pos(), "type %s: use only one of //napi:class, //napi:object and //napi:type", obj.Name())
					continue
				}
				if ts.TypeParams != nil && len(ds) > 0 {
					g.errorf(ts.Pos(), "type %s: generic types cannot be annotated", obj.Name())
					continue
				}
				if d, ok := ds["class"]; ok {
					if obj.IsAlias() {
						g.errorf(d.pos, "//napi:class cannot be used on the alias %s", obj.Name())
						continue
					}
					c := &classModel{pos: ts.Pos(), goName: obj.Name(), jsName: orDefault(d.arg, obj.Name()),
						doc: docText(doc), members: map[string]token.Pos{}}
					g.classes[obj] = c
					g.claim(g.names, "export", c.jsName, c.pos)
					g.claim(g.tsNames, "TypeScript type", c.jsName, c.pos)
				}
				if d, ok := ds["object"]; ok {
					if _, isStruct := obj.Type().Underlying().(*types.Struct); !isStruct {
						g.errorf(d.pos, "//napi:object needs a struct type, %s is not", obj.Name())
						continue
					}
					g.objNames[obj] = orDefault(d.arg, obj.Name())
				}
				if d, ok := ds["type"]; ok {
					if d.arg == "" {
						g.errorf(d.pos, "//napi:type needs the TypeScript type, e.g. //napi:type [number, number]")
						continue
					}
					g.overrides[obj] = d.arg
				}
			}
		}
	}
	g.mod.classes = make([]*classModel, 0, len(g.classes))
	for _, c := range g.classes {
		g.mod.classes = append(g.mod.classes, c)
	}
	sort.Slice(g.mod.classes, func(i, j int) bool { return g.mod.classes[i].pos < g.mod.classes[j].pos })
}

func (g *analyzer) collectFieldDocs(st *ast.StructType) {
	for _, f := range st.Fields.List {
		doc := docText(f.Doc)
		if doc == "" {
			doc = docText(f.Comment)
		}
		for _, name := range f.Names {
			if v, ok := g.info.Defs[name].(*types.Var); ok && doc != "" {
				g.fieldDocs[v] = doc
			}
		}
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// ---- constants ----

func (g *analyzer) collectValues() {
	for _, f := range g.files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				doc := vs.Doc
				if doc == nil && !gd.Lparen.IsValid() {
					doc = gd.Doc
				}
				d, ok := g.allowedDirectives(directives(doc), "a constant or variable", "export")["export"]
				if !ok {
					continue
				}
				if len(vs.Names) != 1 {
					g.errorf(d.pos, "//napi:export needs a declaration with one name")
					continue
				}
				obj := g.info.Defs[vs.Names[0]]
				if obj == nil {
					continue
				}
				c := &constModel{pos: vs.Pos(), goName: obj.Name(), doc: docText(doc)}
				t := types.Default(obj.Type())
				if why := g.check(t, false, true, false, map[types.Type]bool{}); why != "" {
					g.errorf(vs.Pos(), "%s: %s", obj.Name(), why)
					continue
				}
				c.ts = g.ts(t, tsResult)
				if cls, member, ok := g.classMember(d); ok {
					if cls == nil {
						continue
					}
					c.jsName = member
					g.claim(cls.members, "static member of "+cls.jsName, "static "+member, c.pos)
					cls.consts = append(cls.consts, c)
					continue
				}
				c.jsName = orDefault(d.arg, lowerCamel(obj.Name()))
				g.claim(g.names, "export", c.jsName, c.pos)
				g.mod.consts = append(g.mod.consts, c)
			}
		}
	}
}

// classMember parses the argument "Class.member" of //napi:export. ok is false if the argument
// has no dot; cls is nil (with an error reported) if the class is unknown.
func (g *analyzer) classMember(d directive) (cls *classModel, member string, ok bool) {
	className, member, ok := strings.Cut(d.arg, ".")
	if !ok {
		return nil, "", false
	}
	for obj, c := range g.classes {
		if obj.Name() == className {
			return c, member, true
		}
	}
	g.errorf(d.pos, "//napi:export %s: %s is not a //napi:class type", d.arg, className)
	return nil, member, true
}

// ---- functions and methods ----

func (g *analyzer) collectFuncs() {
	type pending struct {
		getter, setter *ast.FuncDecl
		name           string
	}
	accessors := map[*classModel]map[string]*pending{}
	for _, f := range g.files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fn, _ := g.info.Defs[fd.Name].(*types.Func)
			if fn == nil {
				continue
			}
			sig := fn.Type().(*types.Signature)
			if fd.Recv == nil {
				g.plainFunc(fd, fn, sig)
				continue
			}
			cls := g.classOf(sig.Recv().Type())
			ds := directives(fd.Doc)
			if cls == nil {
				if len(ds) > 0 {
					g.errorf(ds[0].pos, "//napi:%s on a method of a type that is not a //napi:class", ds[0].name)
				}
				continue
			}
			dm := g.allowedDirectives(ds, "a method", "export", "getter", "setter", "skip", "returns")
			_, skip := dm["skip"]
			if skip || !fd.Name.IsExported() || isSpecialMethod(fd.Name.Name) {
				continue
			}
			if sig.RecvTypeParams() != nil {
				g.errorf(fd.Pos(), "method %s: generic classes are not supported", fd.Name.Name)
				continue
			}
			get, isGet := dm["getter"]
			set, isSet := dm["setter"]
			if isGet || isSet {
				if isGet && isSet {
					g.errorf(fd.Pos(), "method %s: //napi:getter and //napi:setter on the same method", fd.Name.Name)
					continue
				}
				name := orDefault(get.arg, lowerCamel(fd.Name.Name))
				if isSet {
					name = orDefault(set.arg, lowerCamel(strings.TrimPrefix(fd.Name.Name, "Set")))
				}
				if accessors[cls] == nil {
					accessors[cls] = map[string]*pending{}
				}
				p := accessors[cls][name]
				if p == nil {
					p = &pending{name: name}
					accessors[cls][name] = p
				}
				slot := &p.getter
				if isSet {
					slot = &p.setter
				}
				if *slot != nil {
					g.errorf(fd.Pos(), "class %s: property %q has two %ss", cls.jsName, name, map[bool]string{false: "getter", true: "setter"}[isSet])
					continue
				}
				*slot = fd
				continue
			}
			m := g.callable(fd, fn, sig, "Method", false, dm)
			if m == nil {
				continue
			}
			m.jsName = orDefault(dm["export"].arg, lowerCamel(fd.Name.Name))
			g.claim(cls.members, "member of "+cls.jsName, m.jsName, m.pos)
			cls.methods = append(cls.methods, m)
		}
	}
	for cls, byName := range accessors {
		for _, p := range byName {
			if a := g.accessor(cls, p.name, p.getter, p.setter); a != nil {
				g.claim(cls.members, "member of "+cls.jsName, a.jsName, a.pos)
				cls.accessors = append(cls.accessors, a)
			}
		}
		sort.Slice(cls.accessors, func(i, j int) bool { return cls.accessors[i].pos < cls.accessors[j].pos })
	}
}

// isSpecialMethod reports the methods that napi itself calls: they are never exported.
func isSpecialMethod(name string) bool {
	return name == "Finalize" || name == "FromJS" || name == "ToJS"
}

// classOf returns the class of T for a type T or *T, or nil.
func (g *analyzer) classOf(t types.Type) *classModel {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := types.Unalias(t).(*types.Named); ok {
		return g.classes[n.Origin().Obj()]
	}
	return nil
}

func (g *analyzer) plainFunc(fd *ast.FuncDecl, fn *types.Func, sig *types.Signature) {
	dm := g.allowedDirectives(directives(fd.Doc), "a function", "export", "constructor", "returns")
	exp, isExport := dm["export"]
	_, isCtor := dm["constructor"]
	switch {
	case isExport && isCtor:
		g.errorf(fd.Pos(), "func %s: //napi:export and //napi:constructor on the same function", fn.Name())
	case isCtor:
		var cls *classModel
		if sig.Results().Len() == 2 && isError(sig.Results().At(1).Type()) {
			if _, isPtr := types.Unalias(sig.Results().At(0).Type()).(*types.Pointer); isPtr {
				cls = g.classOf(sig.Results().At(0).Type())
			}
		}
		if cls == nil {
			g.errorf(fd.Pos(), "func %s: a //napi:constructor must return (*T, error), with T a //napi:class type", fn.Name())
			return
		}
		if cls.ctor != nil {
			g.errorf(fd.Pos(), "class %s has two constructors (%s and %s)", cls.jsName, cls.ctor.goName, fn.Name())
			return
		}
		if m := g.callable(fd, fn, sig, "Ctor", false, dm); m != nil {
			cls.ctor = m
		}
	case isExport:
		if cls, member, ok := g.classMember(exp); ok {
			if cls == nil {
				return
			}
			if m := g.callable(fd, fn, sig, "Func", true, dm); m != nil {
				m.jsName = member
				g.claim(cls.members, "static member of "+cls.jsName, "static "+member, m.pos)
				cls.statics = append(cls.statics, m)
			}
			return
		}
		if m := g.callable(fd, fn, sig, "Func", true, dm); m != nil {
			m.jsName = orDefault(exp.arg, lowerCamel(fn.Name()))
			g.claim(g.names, "export", m.jsName, m.pos)
			g.mod.funcs = append(g.mod.funcs, m)
		}
	case len(dm) > 0:
		g.errorf(fd.Pos(), "func %s: //napi:returns needs //napi:export", fn.Name())
	}
}

// callable builds the model of a function (family "Func"), a method ("Method") or a constructor
// ("Ctor"): it checks the signature, picks the napi wrapper and computes the TypeScript types.
func (g *analyzer) callable(fd *ast.FuncDecl, fn *types.Func, sig *types.Signature, family string, allowAsync bool, dm map[string]directive) *funcModel {
	what := "func " + fn.Name()
	if family == "Method" {
		what = "method " + fn.Name()
	}
	fail := func(format string, args ...any) *funcModel {
		g.errorf(fd.Pos(), "%s: %s", what, fmt.Sprintf(format, args...))
		return nil
	}
	if sig.TypeParams() != nil {
		return fail("generic functions cannot be exported")
	}
	if sig.Variadic() {
		return fail("variadic functions are not supported: take a napi.CallContext and read the arguments from it")
	}
	m := &funcModel{pos: fd.Pos(), goName: fn.Name(), doc: docText(fd.Doc)}
	params := sig.Params()
	first, async := 0, false
	if params.Len() > 0 && isNamed(params.At(0).Type(), "context", "Context") {
		if !allowAsync {
			return fail("only plain functions can be asynchronous (context.Context parameter); use Env.Async")
		}
		first, async = 1, true
	}
	n := params.Len() - first
	if n > maxArity {
		return fail("%d parameters: at most %d are supported", n, maxArity)
	}
	used := map[string]bool{}
	for i := first; i < params.Len(); i++ {
		p := params.At(i)
		if isNamed(p.Type(), napiPath, "Env") || isNamed(p.Type(), napiPath, "CallContext") {
			if async {
				return fail("parameter %d (%s) cannot be used in a goroutine", i+1, g.typeString(p.Type()))
			}
			m.rest = m.rest || isNamed(p.Type(), napiPath, "CallContext")
			continue
		}
		if why := g.check(p.Type(), true, false, async, map[types.Type]bool{}); why != "" {
			return fail("parameter %d: %s", i+1, why)
		}
		m.params = append(m.params, paramModel{name: paramName(p.Name(), len(m.params), used), ts: g.ts(p.Type(), tsParam)})
	}
	results := sig.Results()
	var result types.Type
	hasErr := false
	switch results.Len() {
	case 0:
	case 1:
		if isError(results.At(0).Type()) {
			hasErr = true
		} else {
			result = results.At(0).Type()
		}
	case 2:
		if !isError(results.At(1).Type()) {
			return fail("the second result must be error")
		}
		result, hasErr = results.At(0).Type(), true
	default:
		return fail("%d results: at most a value and an error are supported", results.Len())
	}
	if result != nil {
		if why := g.check(result, false, true, async, map[types.Type]bool{}); why != "" {
			return fail("result: %s", why)
		}
	}
	switch {
	case family == "Ctor":
		m.wrapper = "Ctor"
	case async:
		if !hasErr {
			return fail("an asynchronous function must return (R, error) or error")
		}
		m.wrapper = map[bool]string{true: "Async", false: "AsyncProc"}[result != nil]
	default:
		kind := map[bool]string{true: "", false: "Proc"}[result != nil]
		if family == "Func" && result == nil {
			m.wrapper = "Proc"
		} else {
			m.wrapper = family + kind
		}
		if hasErr {
			m.wrapper += "E"
		}
	}
	m.wrapper += fmt.Sprint(n)
	switch {
	case dm["returns"].arg != "":
		m.ret = dm["returns"].arg
	case result == nil:
		m.ret = "void"
	default:
		m.ret = g.ts(result, tsResult)
	}
	if _, overridden := dm["returns"]; async && !overridden {
		m.ret = "Promise<" + m.ret + ">"
	}
	return m
}

func (g *analyzer) accessor(cls *classModel, name string, getter, setter *ast.FuncDecl) *accessorModel {
	if getter == nil {
		g.errorf(setter.Pos(), "class %s: property %q has a setter but no //napi:getter", cls.jsName, name)
		return nil
	}
	gsig := g.info.Defs[getter.Name].Type().(*types.Signature)
	res := gsig.Results()
	ok := gsig.Params().Len() == 0 && !gsig.Variadic() &&
		(res.Len() == 1 && !isError(res.At(0).Type()) || res.Len() == 2 && isError(res.At(1).Type()))
	if !ok {
		g.errorf(getter.Pos(), "getter %s: must have the form func (*T) Name() V or func (*T) Name() (V, error)", getter.Name.Name)
		return nil
	}
	a := &accessorModel{pos: getter.Pos(), jsName: name, getter: getter.Name.Name, doc: docText(getter.Doc),
		fallible: res.Len() == 2, valueType: res.At(0).Type()}
	if why := g.check(a.valueType, setter != nil, true, false, map[types.Type]bool{}); why != "" {
		g.errorf(getter.Pos(), "getter %s: %s", getter.Name.Name, why)
		return nil
	}
	a.ts = g.ts(a.valueType, tsField)
	if setter == nil {
		return a
	}
	ssig := g.info.Defs[setter.Name].Type().(*types.Signature)
	sres := ssig.Results()
	setFallible := sres.Len() == 1 && isError(sres.At(0).Type())
	if ssig.Params().Len() != 1 || ssig.Variadic() || !(sres.Len() == 0 || setFallible) ||
		!types.Identical(ssig.Params().At(0).Type(), a.valueType) {
		g.errorf(setter.Pos(), "setter %s: must have the form func (*T) %s(v %s), optionally returning error",
			setter.Name.Name, setter.Name.Name, g.typeString(a.valueType))
		return nil
	}
	if setFallible != a.fallible {
		g.errorf(setter.Pos(), "property %q: getter and setter must both return an error, or neither", name)
		return nil
	}
	a.setter = setter.Name.Name
	return a
}

// finish emits the annotated objects that no signature uses and orders the model.
func (g *analyzer) finish() {
	for obj := range g.objNames {
		g.object(obj)
	}
	g.mod.objects = make([]*objectModel, 0, len(g.objects))
	for _, o := range g.objects {
		g.mod.objects = append(g.mod.objects, o)
	}
	sort.Slice(g.mod.objects, func(i, j int) bool { return g.mod.objects[i].pos < g.mod.objects[j].pos })
	sort.Slice(g.mod.funcs, func(i, j int) bool { return g.mod.funcs[i].pos < g.mod.funcs[j].pos })
	sort.Slice(g.mod.consts, func(i, j int) bool { return g.mod.consts[i].pos < g.mod.consts[j].pos })
	for _, c := range g.mod.classes {
		for _, list := range [][]*funcModel{c.methods, c.statics} {
			sort.Slice(list, func(i, j int) bool { return list[i].pos < list[j].pos })
		}
		sort.Slice(c.consts, func(i, j int) bool { return c.consts[i].pos < c.consts[j].pos })
	}
}

// ---- helpers on types ----

func isError(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

// isNamed reports whether t is the named type path.name (not a pointer to it).
func isNamed(t types.Type, path, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == path && n.Obj().Name() == name
}

func (g *analyzer) typeString(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == g.pkg {
			return ""
		}
		return p.Name() // as written in source, not the full import path
	})
}

// paramName returns a valid, unique TypeScript parameter name.
func paramName(name string, index int, used map[string]bool) string {
	if name == "" || name == "_" {
		name = fmt.Sprintf("arg%d", index)
	}
	if tsReserved[name] {
		name += "_"
	}
	for used[name] {
		name += "_"
	}
	used[name] = true
	return name
}
