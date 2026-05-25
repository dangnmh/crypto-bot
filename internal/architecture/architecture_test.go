package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanArchitectureImportBoundaries(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	var violations []string

	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".coverage", "bin", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fileViolations, err := importViolations(root, path)
		if err != nil {
			return err
		}
		violations = append(violations, fileViolations...)
		return nil
	}))

	require.Empty(t, violations, "forbidden imports found")
}

func importViolations(root, path string) ([]string, error) {
	imports, err := fileImports(path)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	rel = filepath.ToSlash(rel)

	violations := make([]string, 0)
	for _, imp := range imports {
		if forbiddenImport(rel, imp) {
			violations = append(violations, rel+" imports "+imp)
		}
	}
	return violations, nil
}

func forbiddenImport(rel, importPath string) bool {
	switch {
	case strings.HasPrefix(rel, "internal/domain/"):
		return forbiddenDomainImport(importPath)
	case strings.HasPrefix(rel, "internal/infrastructure/"):
		return strings.HasPrefix(importPath, "crypto-bot/internal/bots/")
	case strings.HasPrefix(rel, "pkg/"):
		return strings.HasPrefix(importPath, "crypto-bot/internal/")
	default:
		return false
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func fileImports(path string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	imports := make([]string, 0, len(file.Imports))
	for _, spec := range file.Imports {
		imports = append(imports, strings.Trim(spec.Path.Value, `"`))
	}
	return imports, nil
}

func forbiddenDomainImport(importPath string) bool {
	return strings.HasPrefix(importPath, "crypto-bot/internal/infrastructure/") ||
		strings.HasPrefix(importPath, "crypto-bot/internal/bots/")
}
