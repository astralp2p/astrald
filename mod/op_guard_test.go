package mod_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestEveryOpIsGuardedOrSaysWhy fails when an op neither asks for a named
// action nor records why it needs none.
//
// An op is a method of a module's *Module whose name starts with "Op"; every
// module registers its ops with AddStructPrefix(mod, "Op"). An op asks for a
// named action when its body, or a function of the same package that it calls,
// calls a method named Authorize. Any other op holds a `// why:` comment in its
// doc comment or body.
//
// note: the scan reads names, not types. A call to any function of the same
// package that calls Authorize counts as a guard, and any `// why:` comment in
// the op counts as its reason.
//
// note: an op added with AddOp instead of AddStructPrefix is not scanned.
func TestEveryOpIsGuardedOrSaysWhy(t *testing.T) {
	missing, ops, err := findUnguardedOps(".")
	if err != nil {
		t.Fatal(err)
	}
	// why: a scan that finds no op passes vacuously, so a change to how modules
	// declare ops must fail here instead.
	if ops == 0 {
		t.Fatal("no ops found under mod/*/src")
	}
	for _, m := range missing {
		t.Errorf("%s: calls no Authorize and has no // why: comment saying why it needs no named action", m)
	}
}

// TestFindUnguardedOpsReportsOnlyTheUnguardedOp runs the scan over a fixture
// module holding one op of each kind.
func TestFindUnguardedOpsReportsOnlyTheUnguardedOp(t *testing.T) {
	missing, ops, err := findUnguardedOps("testdata/opguard")
	if err != nil {
		t.Fatal(err)
	}
	if ops != 5 {
		t.Errorf("found %d ops, want 5", ops)
	}
	want := []string{filepath.Join("testdata", "opguard", "fixture", "src", "ops.go") + ":41: OpUnguarded"}
	if !slices.Equal(missing, want) {
		t.Errorf("missing = %q, want %q", missing, want)
	}
}

// findUnguardedOps scans every root/*/src package and returns the position of
// each op that calls no Authorize and has no `// why:` comment, sorted, and the
// number of ops scanned.
func findUnguardedOps(root string) (missing []string, ops int, err error) {
	dirs, err := filepath.Glob(filepath.Join(root, "*", "src"))
	if err != nil {
		return nil, 0, err
	}
	for _, dir := range dirs {
		p, err := parsePackage(dir)
		if err != nil {
			return nil, 0, err
		}
		for _, op := range p.ops {
			ops++
			if p.authorizes(op.Name.Name) || hasWhy(p.comments(op)) {
				continue
			}
			pos := p.fset.Position(op.Pos())
			missing = append(missing, fmt.Sprintf("%s:%d: %s", pos.Filename, pos.Line, op.Name.Name))
		}
	}
	slices.Sort(missing)
	return missing, ops, nil
}

// pkg is one module's src package: its ops and every function declared in it,
// keyed by name.
//
// why: methods are keyed by name alone, whatever their receiver; a module's
// guards are methods of *Module, and resolving receivers needs type checking.
type pkg struct {
	fset  *token.FileSet
	files []*ast.File
	ops   []*ast.FuncDecl
	funcs map[string][]*ast.FuncDecl
	memo  map[string]bool
}

func parsePackage(dir string) (*pkg, error) {
	p := &pkg{fset: token.NewFileSet(), funcs: map[string][]*ast.FuncDecl{}, memo: map[string]bool{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(p.fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		p.files = append(p.files, f)
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			p.funcs[fn.Name.Name] = append(p.funcs[fn.Name.Name], fn)
			if isOp(fn) {
				p.ops = append(p.ops, fn)
			}
		}
	}
	return p, nil
}

// isOp reports whether fn is a method of *Module named Op<Name>, the methods
// AddStructPrefix(mod, "Op") registers.
func isOp(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	recv, ok := star.X.(*ast.Ident)
	if !ok || recv.Name != "Module" {
		return false
	}
	name := fn.Name.Name
	return len(name) > 2 && strings.HasPrefix(name, "Op") && ast.IsExported(name[2:])
}

// authorizes reports whether a function of this package named name calls
// Authorize, directly or through other functions of this package.
func (p *pkg) authorizes(name string) bool {
	if v, ok := p.memo[name]; ok {
		return v
	}
	// why: a function on the current call path counts as not authorizing until
	// its body is read, so a recursive call ends the walk.
	p.memo[name] = false
	for _, fn := range p.funcs[name] {
		if p.bodyAuthorizes(fn.Body) {
			p.memo[name] = true
			return true
		}
	}
	return false
}

// bodyAuthorizes reports whether body calls Authorize, or a function of this
// package that authorizes. Function literals in body count as body.
func (p *pkg) bodyAuthorizes(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee := calleeName(call)
		if callee == "Authorize" || (callee != "" && p.authorizes(callee)) {
			found = true
		}
		return !found
	})
	return found
}

// calleeName returns the called function's name, or "" for a call through
// anything but a name or a selector.
func calleeName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}
	return ""
}

// comments returns fn's doc comment and every comment group inside fn.
func (p *pkg) comments(fn *ast.FuncDecl) []*ast.CommentGroup {
	var out []*ast.CommentGroup
	if fn.Doc != nil {
		out = append(out, fn.Doc)
	}
	for _, f := range p.files {
		for _, cg := range f.Comments {
			if cg.Pos() >= fn.Pos() && cg.End() <= fn.End() {
				out = append(out, cg)
			}
		}
	}
	return out
}

// hasWhy reports whether a comment line in groups starts with the `why:` tag.
func hasWhy(groups []*ast.CommentGroup) bool {
	for _, cg := range groups {
		for _, c := range cg.List {
			text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
			if strings.HasPrefix(text, "why:") {
				return true
			}
		}
	}
	return false
}
