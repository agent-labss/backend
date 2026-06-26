package architecture

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const modulePath = "ai/backend"

var allowedPackages = []string{
	modulePath + "/cmd/server",
	modulePath + "/internal/agent",
	modulePath + "/internal/app",
	modulePath + "/internal/architecture",
	modulePath + "/internal/config",
	modulePath + "/internal/database",
	modulePath + "/internal/database/generated",
	modulePath + "/internal/database/queryinput",
	modulePath + "/internal/httpapi",
	modulePath + "/internal/platform/datastore",
	modulePath + "/internal/platform/sqlite",
	modulePath + "/internal/status",
	modulePath + "/internal/toolcatalog",
}

var forbiddenProductionSQLPatterns = []string{
	".Clauses(",
	".Count(",
	".Delete(",
	".Exec(",
	".Find(",
	".First(",
	".Last(",
	".Model(",
	".Order(",
	".Raw(",
	".Row(",
	".Rows(",
	".Save(",
	".Scan(",
	".Table(",
	".Take(",
	".Update(",
	".Updates(",
	".Where(",
	"gorm.WithResult(",
	"gorm.io/cli/gorm/field",
	"gorm.io/cli/gorm/typed",
}

var databaseAccessImports = []string{
	`"ai/backend/internal/database/generated"`,
	`"gorm.io/gorm"`,
	`"gorm.io/cli/gorm/field"`,
	`"gorm.io/cli/gorm/typed"`,
}

type packageInfo struct {
	ImportPath string
	Imports    []string
}

func TestPackagesRemainExplicitlyAllowed(t *testing.T) {
	packages := loadPackages(t)

	for _, pkg := range packages {
		if !slices.Contains(allowedPackages, pkg.ImportPath) {
			t.Fatalf("package %q is not in the architecture allowlist", pkg.ImportPath)
		}
	}
}

func TestPackageBoundaries(t *testing.T) {
	packages := loadPackages(t)

	assertDoesNotImport(t, packages, modulePath+"/internal/httpapi", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/status", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/status", modulePath+"/internal/platform/datastore")
	assertDoesNotImport(t, packages, modulePath+"/internal/status", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/datastore", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/datastore", modulePath+"/internal/status")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/sqlite", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/sqlite", modulePath+"/internal/status")
	assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/agent")
	assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/platform/datastore")
	assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/agent", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/agent", modulePath+"/internal/platform/datastore")
	assertDoesNotImport(t, packages, modulePath+"/internal/agent", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/datastore", modulePath+"/internal/agent")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/datastore", modulePath+"/internal/toolcatalog")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/sqlite", modulePath+"/internal/agent")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/sqlite", modulePath+"/internal/toolcatalog")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/app")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/platform/datastore")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/status")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/platform/datastore")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/status")
}

func TestProductionSQLUsesGeneratedQueries(t *testing.T) {
	for _, source := range productionGoFiles(t) {
		forbidden := firstForbiddenProductionSQLPattern(t, source)
		if forbidden != "" {
			t.Fatalf("%s contains %q; put custom SQL in internal/database/queryinput and use internal/database/generated", source, forbidden)
		}
	}
}

func loadPackages(t *testing.T) []packageInfo {
	t.Helper()

	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = "../.."

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			t.Fatalf("go list failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		t.Fatalf("go list failed: %v", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(output))
	var packages []packageInfo
	for {
		var pkg packageInfo
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode go list package: %v", err)
		}
		packages = append(packages, pkg)
	}

	return packages
}

func assertDoesNotImport(t *testing.T, packages []packageInfo, source string, forbidden string) {
	t.Helper()

	for _, pkg := range packages {
		if pkg.ImportPath != source {
			continue
		}
		if slices.Contains(pkg.Imports, forbidden) {
			t.Fatalf("%s must not import %s", source, forbidden)
		}
	}
}

func productionGoFiles(t *testing.T) []string {
	t.Helper()

	files, err := walkProductionGoFiles()
	if err != nil {
		t.Fatalf("walk production go files: %v", err)
	}
	return files
}

func walkProductionGoFiles() ([]string, error) {
	var files []string
	err := filepath.WalkDir("../..", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skipProductionDir(path, entry) {
			return filepath.SkipDir
		}
		if includeProductionGoFile(path, entry) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk source tree: %w", err)
	}
	return files, nil
}

func skipProductionDir(path string, entry os.DirEntry) bool {
	if !entry.IsDir() {
		return false
	}
	return path == "../../.git" ||
		path == "../../.codegraph" ||
		path == "../../.worktrees" ||
		path == "../../internal/database/generated"
}

func includeProductionGoFile(path string, entry os.DirEntry) bool {
	if entry.IsDir() {
		return false
	}
	if strings.HasPrefix(path, "../../internal/platform/sqlite/") {
		return false
	}
	return strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go")
}

func firstForbiddenProductionSQLPattern(t *testing.T, source string) string {
	t.Helper()

	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read %s: %v", source, err)
	}
	text := string(content)
	if !containsAny(text, databaseAccessImports) {
		return ""
	}
	for _, forbidden := range forbiddenProductionSQLPatterns {
		if strings.Contains(text, forbidden) {
			return forbidden
		}
	}
	return ""
}

func containsAny(text string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}
