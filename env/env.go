// Package env loads .env into the process environment. It has no imports from this module
// and is blank-imported by every package that reads os.Getenv in init(), so Go initialises it
// before them: package init order follows imports, and loading .env from a package that
// itself imports those packages (as the template's utils does) runs too late for them.
package env

import (
	"os"

	"github.com/joho/godotenv"
)

func init() {

	if _, err := os.Stat(".env"); err == nil {
		err = godotenv.Load(".env")
		if err != nil {
			panic(err)
		}
	}
}
