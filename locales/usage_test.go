package locales_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// moduleRoot is the repository root, one level above this package.
const moduleRoot = ".."

// sourceFile is one parsed Go file of the app.
type sourceFile struct {
	path string
	file *ast.File
	fset *token.FileSet
}

// appSources parses the app's Go files outside tests and the generated
// locales package.
func appSources(t *testing.T) []sourceFile {
	t.Helper()
	var files []sourceFile
	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "locales", "build", "node_modules", ".git":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		files = append(files, sourceFile{path: path, file: f, fset: fset})
		return nil
	})
	if err != nil {
		t.Fatalf("read the app's sources: %v", err)
	}
	return files
}

// isLocalesCall says whether e is a call of a function of package locales.
func isLocalesCall(e ast.Node) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "locales" {
		return "", false
	}
	return sel.Sel.Name, true
}

// TestNoMessageAtPackageLevel fails on a message said where a variable or a
// constant is declared: it would be said once, as the program starts, in
// whichever language was shown then, and stay so when the language changes.
func TestNoMessageAtPackageLevel(t *testing.T) {
	for _, src := range appSources(t) {
		for _, decl := range src.file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.VAR && gen.Tok != token.CONST) {
				continue
			}
			ast.Inspect(gen, func(n ast.Node) bool {
				if _, isFunc := n.(*ast.FuncLit); isFunc {
					return false
				}
				if name, ok := isLocalesCall(n); ok {
					t.Errorf("%s: locales.%s is said at package level; say it in a function", src.fset.Position(n.Pos()), name)
				}
				return true
			})
		}
	}
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// TestNoChineseInTheInterface fails on Chinese text typed into the handler:
// what the interface says is a message of the catalogs. An element's key
// names it for the frame to frame diff and is never shown.
func TestNoChineseInTheInterface(t *testing.T) {
	for _, src := range appSources(t) {
		if !strings.Contains(filepath.ToSlash(src.path), "internal/handler/") {
			continue
		}
		var stack []ast.Node
		ast.Inspect(src.file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || !hasHan(lit.Value) {
				return true
			}
			if len(stack) >= 2 {
				if call, ok := stack[len(stack)-2].(*ast.CallExpr); ok {
					if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Key" {
						return true
					}
				}
			}
			t.Errorf("%s: %s is typed into the interface; add it to the catalogs", src.fset.Position(lit.Pos()), lit.Value)
			return true
		})
	}
}

// TestEveryMessageIsUsed fails on a message of the catalogs nothing says.
func TestEveryMessageIsUsed(t *testing.T) {
	raw, err := os.ReadFile("en.json")
	if err != nil {
		t.Fatalf("read en.json: %v", err)
	}
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parse en.json: %v", err)
	}
	used := map[string]bool{}
	for _, src := range appSources(t) {
		ast.Inspect(src.file, func(n ast.Node) bool {
			if name, ok := isLocalesCall(n); ok {
				used[name] = true
			}
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "locales" {
					used[sel.Sel.Name] = true
				}
			}
			return true
		})
	}
	for key := range catalog {
		if strings.HasPrefix(key, "@") {
			continue
		}
		function := strings.ToUpper(key[:1]) + key[1:]
		if !used[function] {
			t.Errorf("message %q is in the catalogs, and nothing says it", key)
		}
	}
}
