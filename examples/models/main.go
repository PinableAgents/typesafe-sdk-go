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

var exitProcess = os.Exit

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		exitProcess(1)
	}
}
func run() error {
	mock := flag.Bool("mock", false, "use local fixture instead of the live API")
	flag.Parse()
	cfg := typesafe.Config{}
	if *mock {
		s := mockapi.New()
		defer s.Close()
		cfg.APIKey = "local-placeholder"
		cfg.BaseURL = s.URL
		cfg.AllowInsecureHTTP = true
		fmt.Fprintln(os.Stderr, "MOCK MODE")
	}
	c, err := typesafe.NewClient(cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := c.Models().List(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(response)
