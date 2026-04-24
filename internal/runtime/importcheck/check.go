package importcheck

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type Violation struct {
	File   string
	Import string
	Reason string
}

func Check(root string) ([]Violation, error) {
	var violations []Violation

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if shouldSkip(path, entry) {
			return filepath.SkipDir
		}

		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		fileViolations, parseErr := checkFile(path)
		if parseErr != nil {
			return parseErr
		}

		violations = append(violations, fileViolations...)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return violations, nil
}

func shouldSkip(path string, entry os.DirEntry) bool {
	if !entry.IsDir() {
		return false
	}

	name := entry.Name()
	return name == ".git" || name == ".context" || name == ".haft" || name == "bin"
}

func checkFile(path string) ([]Violation, error) {
	fileset := token.NewFileSet()
	file, err := parser.ParseFile(fileset, path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}

	layer := layerFor(path)
	if layer == "" {
		return nil, nil
	}

	return checkImports(path, layer, file.Imports), nil
}

func checkImports(path string, layer string, imports []*ast.ImportSpec) []Violation {
	violations := []Violation{}

	for _, spec := range imports {
		importPath := strings.Trim(spec.Path.Value, "\"")
		reason := forbiddenReason(layer, importPath)
		if reason != "" {
			violations = append(violations, Violation{
				File:   path,
				Import: importPath,
				Reason: reason,
			})
		}
	}

	return violations
}

func layerFor(path string) string {
	switch {
	case strings.Contains(path, "internal/kernel/"):
		return "kernel"
	case strings.Contains(path, "internal/domain/"):
		return "domain"
	case strings.Contains(path, "internal/plans/"):
		return "plans"
	case strings.Contains(path, "internal/app/"):
		return "app"
	case strings.Contains(path, "internal/adapters/"):
		return "adapters"
	}

	return ""
}

func forbiddenReason(layer string, importPath string) string {
	projectImport := strings.HasPrefix(importPath, "github.com/ivanzakutnii/error-tracker/internal/")
	if !projectImport {
		return ""
	}

	switch layer {
	case "kernel":
		return "kernel cannot import project packages"
	case "domain":
		return rejectAbove(importPath, "domain", "internal/adapters/", "internal/app/", "internal/runtime/")
	case "plans":
		return rejectAbove(importPath, "plans", "internal/adapters/", "internal/app/", "internal/runtime/")
	case "app":
		return rejectAbove(importPath, "app", "internal/adapters/", "internal/runtime/")
	}

	return ""
}

func rejectAbove(importPath string, layer string, prefixes ...string) string {
	for _, prefix := range prefixes {
		if strings.Contains(importPath, prefix) {
			return layer + " cannot import " + prefix
		}
	}

	return ""
}

func ErrViolations(violations []Violation) error {
	if len(violations) == 0 {
		return nil
	}

	return errors.New("import boundary violations found")
}
