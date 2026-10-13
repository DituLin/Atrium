// atrium-spool-read is a host-only bounded FD reader. It accepts no model input.
package main

import (
	"flag"
	"github.com/DituLin/Atrium/internal/archivespool"
	"io"
	"os"
)

func main() {
	root := flag.String("root", "", "trusted allowed root")
	path := flag.String("relative", "", "relative native source")
	flag.Parse()
	if flag.NArg() != 0 {
		os.Exit(1)
	}
	f, err := archivespool.Open(*root, *path)
	if err != nil {
		os.Exit(1)
	}
	defer func() { _ = f.Close() }() // Read-only source cleanup; read errors are handled separately.
	n, err := io.Copy(os.Stdout, io.LimitReader(f, (32<<20)+1))
	if err != nil || n == 0 || n > 32<<20 {
		os.Exit(1)
	}
}
