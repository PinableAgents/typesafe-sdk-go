// typesafe-tool is a one-shot JSON CLI for hosts that can execute a subprocess.
// It is NOT an MCP server or a streaming JSON-RPC service.
package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
	"github.com/PinableAgents/typesafe-sdk-go/contrib/agenttool"
)

var exitProcess = os.Exit

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout)
	stop() // os.Exit does not run deferred functions.
	exitProcess(code)
}

// Stdout always contains JSON only. No key, payload, raw error, or HTTP header
// is logged; a nonzero exit code means callers must not treat data as approval.
func run(ctx context.Context, args []string, input io.Reader, output io.Writer) int {
	return runWith(ctx, args, input, output, typesafe.NewClient, agenttool.New)
}

func runWith(ctx context.Context, args []string, input io.Reader, output io.Writer,
	newClient func(typesafe.Config) (*typesafe.Client, error),
	newRegistry func(agenttool.API) (*agenttool.Registry, error)) int {
	emit := func(value any, code int) int {
		if json.NewEncoder(output).Encode(value) != nil {
			return 1
		}
		return code
	}
	if len(args) == 1 && args[0] == "--list" {
		return emit(map[string]any{"tools": agenttool.Definitions()}, 0)
	}
	if len(args) != 0 {
		return emit(agenttool.Result{Error: &agenttool.ToolError{Code: "usage", Message: "Use --list, or send one JSON tool invocation to stdin.", RequiresReview: true}}, 2)
	}
	raw, err := io.ReadAll(io.LimitReader(input, agenttool.MaxArgumentBytes+1))
	if err != nil || len(raw) > agenttool.MaxArgumentBytes || !json.Valid(raw) {
		return emit(agenttool.Result{Error: &agenttool.ToolError{Code: "invalid_arguments", Message: "Provide one JSON invocation of at most 64 KiB.", RequiresReview: true}}, 2)
	}
	// The host supplies TYPESAFE_API_KEY. Tool arguments cannot select a host
	// or inherit an untrusted TYPESAFE_BASE_URL redirecting the credential.
	client, err := newClient(typesafe.Config{BaseURL: typesafe.DefaultBaseURL, Retry: &typesafe.RetryPolicy{}, MaxResponseBytes: 1 << 20})
	if err != nil {
		return emit(agenttool.Result{Error: &agenttool.ToolError{Code: "configuration_error", Message: "Configure TYPESAFE_API_KEY in the host environment.", RequiresReview: true}}, 2)
	}
	defer client.Close()
	tools, err := newRegistry(client)
	if err != nil {
		return emit(agenttool.ErrorResult(err), 2)
	}
	result := tools.Invoke(ctx, raw)
	if !result.OK {
		return emit(result, 1)
	}
	return emit(result, 0)
}
