// home-mcp runs the household tool adapter using stdin/stdout exclusively.
package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/DituLin/Atrium/internal/homemcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	f := flag.NewFlagSet("home-mcp", flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	path := f.String("config", "home-mcp.yaml", "MCP connection configuration")
	if f.Parse(os.Args[1:]) != nil || f.NArg() != 0 {
		os.Exit(2)
	}
	c, e := homemcp.LoadConfig(*path)
	if e != nil {
		fmt.Fprintln(os.Stderr, "home-mcp: invalid configuration")
		os.Exit(1)
	}
	s, e := homemcp.NewServer(c)
	if e != nil {
		fmt.Fprintln(os.Stderr, "home-mcp: connection configuration rejected")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if e = s.Run(ctx, &mcp.StdioTransport{}); e != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "home-mcp: protocol session ended")
		os.Exit(1)
	}
}
