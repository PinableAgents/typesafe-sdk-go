package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
	"github.com/PinableAgents/typesafe-sdk-go/contrib/agentpolicy"
	"github.com/PinableAgents/typesafe-sdk-go/internal/mockapi"
)

var exitProcess = os.Exit

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exitProcess(1)
	}
}
func run() error { return runWith(agentpolicy.DefaultConfig()) }

func runWith(policy agentpolicy.Config) error {
	mock := flag.Bool("mock", false, "use fixed data; not a model evaluation")
	text := flag.String("text", "请解释 Go context 取消如何传递，不修改文件。", "task summary sent to the evaluator")
	flag.Parse()
	cfg := typesafe.Config{}
	if *mock {
		s := mockapi.New()
		defer s.Close()
		cfg.APIKey = "local-placeholder"
		cfg.BaseURL = s.URL
		cfg.AllowInsecureHTTP = true
		fmt.Fprintln(os.Stderr, "MOCK MODE: always returns the same read-only fixture, regardless of input")
	}
	client, err := typesafe.NewClient(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	router, err := agentpolicy.NewRouter(client, policy)
	if err != nil {
		return err
	}
	decision, evalErr := router.Evaluate(context.Background(), agentpolicy.Task{Text: *text})
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(decision); err != nil {
		return err
	}
	// No harness or tool is executed. A host may observe this result in shadow
	// mode and retain its existing routing, permissions and approval checks.
	return evalErr
}
