package inventory

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"
)

// TestEveryOperationAuthorizes replaces the property the route middleware used
// to give us for free.
//
// With `authz.Require(action)` declared beside each path, a route that forgot
// its permission did not silently inherit "any signed-in user may" — it had no
// permission and was refused. Moving the check into this package (design P7) is
// right for every other reason and loses that: a new function here that forgets
// to authorize is simply unguarded, and nothing says so.
//
// So the property is asserted instead, by reading this package's own source. A
// function added tomorrow is covered without anyone remembering.
func TestEveryOperationAuthorizes(t *testing.T) {
	// Functions that authorize nothing themselves, each with the reason — either
	// they touch no object, or they delegate to something here that does.
	// Adding to this list should be a decision somebody makes on purpose.
	exempt := map[string]string{
		"CAALines":         "pure formatting of configured issuer identifiers; touches no object and reads nothing about the caller",
		"DelegationTarget": "pure formatting of an alias the caller was already authorized to have",
		"ListProjects":     "delegates wholesale to ListProjectsPage, which compiles the caller's grants into the query",
	}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	pkg, ok := pkgs["inventory"]
	if !ok {
		t.Fatal("package inventory not found")
	}

	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() || fn.Body == nil {
				continue
			}
			var takesSubject bool
			for _, p := range fn.Type.Params.List {
				if types(p.Type) == "authz.Subject" {
					takesSubject = true
				}
			}
			if !takesSubject {
				if _, allowed := exempt[fn.Name.Name]; allowed {
					continue
				}
				t.Errorf("%s takes no authz.Subject, so it cannot authorize anything.\n"+
					"Either give it one and check, or add it to the exempt list with the reason.", fn.Name.Name)
				continue
			}
			if !callsAuthorizer(fn) {
				// Delegation counts, but only when it is written down: a
				// function that hands its subject to another in this package is
				// authorizing through it, and the reason names which one.
				if why, allowed := exempt[fn.Name.Name]; allowed && why != "" {
					continue
				}
				t.Errorf("%s takes a subject and never authorizes with it.\n"+
					"An unused subject is worse than no subject: it reads as a check that is not there.\n"+
					"If it delegates, add it to the exempt list naming what it delegates to.", fn.Name.Name)
			}
		}
	}
}

// callsAuthorizer reports whether the function body calls one of this package's
// gates.
func callsAuthorizer(fn *ast.FuncDecl) bool {
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "authorize", "authorizeInAccount", "requireUnrestricted":
				found = true
			case "acl":
				// A listing authorizes by COMPILING the caller's grants into
				// the query rather than by checking before it (design P13).
				// There is nothing to check against — the answer is per row,
				// and the predicate is what applies it.
				found = true
			}
		}
		return true
	})
	return found
}

func types(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + types(t.X)
	case *ast.SelectorExpr:
		return types(t.X) + "." + t.Sel.Name
	}
	return ""
}
