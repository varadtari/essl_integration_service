// Package env loads .env into the process environment. It has no imports from this module
// and is blank-imported by every package that reads os.Getenv in init(), so Go initialises it
// before them: package init order follows imports, and loading .env from a package that
// itself imports those packages (as the template's utils does) runs too late for them.
package env

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func init() {

	if envPath := findEnvFile(); envPath != "" {
		if err := godotenv.Load(envPath); err != nil {
			panic(err)
		}
	}
}

// findEnvFile looks for .env in the working directory and its parents, stopping at the module
// root. `go run .` starts at the root, but `go test` runs each package from its own directory,
// and tests need the same local database settings. Deployed containers have no .env at all.
func findEnvFile() string {

	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		candidate := filepath.Join(dir, ".env")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return ""
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
