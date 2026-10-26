// Command viewer runs `rexkit scrollback` alone, for the live tests: they
// must not depend on the rest of the rexkit binary building.
package main

import (
	"fmt"
	"os"

	"rexkit/internal/app/scrollback"
)

func main() {
	if err := scrollback.Main(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "rexkit:", err)
		os.Exit(1)
	}
}
