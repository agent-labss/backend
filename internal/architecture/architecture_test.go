package architecture

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

const modulePath = "orderbuddy-ai/backend"

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
	modulePath + "/internal/platform/db",
	modulePath + "/internal/platform/sqlite",
	modulePath + "/internal/status",
	modulePath + "/internal/toolcatalog",
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
	assertDoesNotImport(t, packages, modulePath+"/internal/status", modulePath+"/internal/platform/db")
	assertDoesNotImport(t, packages, modulePath+"/internal/status", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/db", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/db", modulePath+"/internal/status")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/sqlite", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/sqlite", modulePath+"/internal/status")
	assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/agent")
	assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/platform/db")
	assertDoesNotImport(t, packages, modulePath+"/internal/toolcatalog", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/agent", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/agent", modulePath+"/internal/platform/db")
	assertDoesNotImport(t, packages, modulePath+"/internal/agent", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/db", modulePath+"/internal/agent")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/db", modulePath+"/internal/toolcatalog")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/sqlite", modulePath+"/internal/agent")
	assertDoesNotImport(t, packages, modulePath+"/internal/platform/sqlite", modulePath+"/internal/toolcatalog")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/app")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/platform/db")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/internal/config", modulePath+"/internal/status")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/httpapi")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/platform/db")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/platform/sqlite")
	assertDoesNotImport(t, packages, modulePath+"/cmd/server", modulePath+"/internal/status")
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
