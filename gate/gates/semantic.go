package gates

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/openagentplatform/openagentplatform/gate"
)

// SemanticScan performs lightweight semantic checks on Go source files.
type SemanticScan struct{}

// NewSemanticScan creates a semantic analysis gate.
func NewSemanticScan() *SemanticScan { return &SemanticScan{} }

func (s *SemanticScan) Name() string { return "semantic" }

// Check reports empty functions and statements after unconditional terminators.
func (s *SemanticScan) Check(ctx context.Context, paths []string) ([]gate.Finding, error) {
	files, err := expandPaths(paths)
	if err != nil {
		return nil, err
	}
	var findings []gate.Finding
	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			continue
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return findings, readErr
		}
		set := token.NewFileSet()
		file, parseErr := parser.ParseFile(set, path, content, 0)
		if parseErr != nil {
			return findings, parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			block, ok := node.(*ast.BlockStmt)
			if ok {
				findings = append(findings, s.checkBlock(set, path, block)...)
			}
			return true
		})
	}
	return findings, nil
}

func (s *SemanticScan) checkBlock(set *token.FileSet, path string, block *ast.BlockStmt) []gate.Finding {
	if len(block.List) == 0 {
		position := set.Position(block.Lbrace)
		return []gate.Finding{{
			Gate: s.Name(), Path: path, Line: position.Line, Column: position.Column,
			Severity: gate.SeverityWarning, Message: "empty code block", Rule: "empty-block",
		}}
	}
	for i, statement := range block.List[:len(block.List)-1] {
		if !terminates(statement) {
			continue
		}
		position := set.Position(block.List[i+1].Pos())
		return []gate.Finding{{
			Gate: s.Name(), Path: path, Line: position.Line, Column: position.Column,
			Severity: gate.SeverityError, Message: "unreachable statement", Rule: "unreachable-code",
		}}
	}
	return nil
}

func terminates(statement ast.Stmt) bool {
	switch value := statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return value.Tok == token.GOTO || value.Tok == token.FALLTHROUGH
	case *ast.ExprStmt:
		call, ok := value.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		identifier, ok := selector.X.(*ast.Ident)
		return ok && identifier.Name == "os" && selector.Sel.Name == "Exit"
	default:
		return false
	}
}
