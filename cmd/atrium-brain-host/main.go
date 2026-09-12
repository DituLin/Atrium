// atrium-brain-host is private stdio IPC for the OpenClaw Atrium plugin.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/DituLin/Atrium/internal/brain"
	"github.com/DituLin/Atrium/internal/brainhost"
	"github.com/DituLin/Atrium/internal/homemcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "atrium-brain-host:", err)
		os.Exit(1)
	}
}
func run() error {
	flags := flag.NewFlagSet("atrium-brain-host", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	binary := flags.String("mcp-bin", "", "absolute home-mcp executable")
	config := flags.String("mcp-config", "", "absolute home-mcp configuration")
	ledgerPath := flags.String("ledger", "", "private Brain ledger")
	principal := flags.String("principal-id", "", "fixed service principal ID")
	toolsOnly := flags.Bool("tools-json", false, "print projected model tool definitions and exit")
	recoverOnly := flags.Bool("recover", false, "query one pending action page without resubmission")
	after := flags.String("recover-after", "", "operation cursor from previous recovery page")
	if flags.Parse(os.Args[1:]) != nil || flags.NArg() != 0 {
		return errors.New("invalid command flags")
	}
	if !filepath.IsAbs(*binary) || !filepath.IsAbs(*config) {
		return errors.New("absolute MCP paths required")
	}
	cfg, err := homemcp.LoadConfig(*config)
	if err != nil {
		return errors.New("invalid MCP configuration")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	input, err := duplicatePipe(ctx, 0)
	if err != nil {
		return errors.New("cannot open runtime input")
	}
	defer func() { _ = input.Close() }()
	output, err := duplicatePipe(ctx, 1)
	if err != nil {
		return errors.New("cannot open runtime output")
	}
	defer func() { _ = output.Close() }()
	command := exec.Command(*binary, "--config", *config) // #nosec G204 -- trusted operator configuration, never model input.
	command.Stderr = os.Stderr
	client, err := mcp.NewClient(&mcp.Implementation{Name: "atrium-brain-host", Version: "1"}, nil).Connect(ctx, &mcp.CommandTransport{Command: command}, nil)
	if err != nil {
		return errors.New("MCP initialization failed")
	}
	defer func() { _ = client.Close() }()
	inventory, err := client.ListTools(ctx, nil)
	if err != nil {
		return errors.New("MCP discovery failed")
	}
	tools, err := brain.ModelTools(inventory.Tools)
	if err != nil || len(tools) != 10 {
		return errors.New("unexpected model tool inventory")
	}
	if *toolsOnly {
		return json.NewEncoder(output).Encode(tools)
	}
	if !filepath.IsAbs(*ledgerPath) {
		return errors.New("absolute private ledger path required")
	}
	scope, err := brain.ScopeFor(cfg.CoreURL, *principal)
	if err != nil {
		return errors.New("invalid ledger scope")
	}
	ledger, err := brain.OpenLedger(*ledgerPath, scope, nil)
	if err != nil {
		return errors.New("cannot open private Brain ledger")
	}
	defer func() { _ = ledger.Close() }()
	var receipt *brain.Receipt
	receiptPath, invocation := os.Getenv("ATRIUM_BRAIN_RECEIPT_FILE"), os.Getenv("ATRIUM_BRAIN_INVOCATION_ID")
	if receiptPath != "" || invocation != "" {
		receipt, err = brain.OpenReceipt(receiptPath, invocation)
		if err != nil {
			return errors.New("invalid private receipt configuration")
		}
		defer func() { _ = receipt.Close() }()
	}
	executor := brain.NewExecutor(client, ledger)
	if *recoverOnly {
		results, next, err := executor.RecoverPage(ctx, *after)
		return brainhost.WriteRecovery(output, results, next, err)
	}
	return brainhost.Serve(ctx, input, output, brain.NewRuntimeWithReceipt(executor, receipt), tools)
}
