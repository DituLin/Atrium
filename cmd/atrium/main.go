// Command atrium runs the Atrium home hub core service and its admin CLI.
package main

import (
	"os"

	"github.com/DituLin/Atritum/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
