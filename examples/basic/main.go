package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
	"github.com/PinableAgents/typesafe-sdk-go/internal/mockapi"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	mock := flag.Bool("mock", false, "use local fixed data; no key or remote API required")
	text := flag.String("text", "Explain how Go context cancellation works.", "content to evaluate")
	flag.Parse()
	cfg := typesafe.Config{}
	if *mock {
		server := mockapi.New()
		defer server.Close()
		cfg.APIKey = "local-placeholder"
		cfg.BaseURL = server.URL
		cfg.AllowInsecureHTTP = true
		fmt.Fprintln(os.Stderr, "MOCK MODE: fixed test data; no model inference or billing")
	}
	client, err := typesafe.NewClient(cfg)
	if err != nil {
		return err
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req := typesafe.SystemOneRequest{State: map[string]string{"task": *text}, Questions: typesafe.Questions{
		"intent":       typesafe.ChoiceLabels("Which task is requested?", "explain", "change", "unknown"),
		"scope":        typesafe.ScoreLevels("How broad is the task?", "One local issue.", "One component.", "Several components."),
		"write_intent": typesafe.Noul{Instructions: "Does the task request persistent changes?"},
	}}
	response, err := client.SystemOne(ctx, req)
	if err != nil {
		return err
	}
	if err := response.ValidateFor(req.Questions); err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(response)
}
