package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
	"github.com/PinableAgents/typesafe-sdk-go/contrib/agenttool"
	"github.com/PinableAgents/typesafe-sdk-go/internal/mockapi"
)

type failingIO struct{}

func (failingIO) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (failingIO) Read([]byte) (int, error)  { return 0, io.ErrClosedPipe }

func TestCLIIOFailures(t *testing.T) {
	if code := run(context.Background(), []string{"--list"}, strings.NewReader(""), failingIO{}); code != 1 {
		t.Fatal("output failure hidden", code)
	}
	var output bytes.Buffer
	if code := run(context.Background(), nil, failingIO{}, &output); code != 2 || !strings.Contains(output.String(), "invalid_arguments") {
		t.Fatal(code, output.String())
	}
}
func TestCLIConstructorsAndSuccessfulInvocation(t *testing.T) {
	server := mockapi.New()
	defer server.Close()
	factory := func(cfg typesafe.Config) (*typesafe.Client, error) {
		if cfg.BaseURL != typesafe.DefaultBaseURL || cfg.Retry == nil || cfg.Retry.MaxRetries != 0 {
			t.Fatal("untrusted defaults")
		}
		cfg.BaseURL = server.URL
		cfg.AllowInsecureHTTP = true
		cfg.APIKey = "fixture-key"
		return typesafe.NewClient(cfg)
	}
	input := `{"tool":"typesafe_list_models","arguments":{}}`
	var output bytes.Buffer
	if code := runWith(context.Background(), nil, strings.NewReader(input), &output, factory, agenttool.New); code != 0 || !strings.Contains(output.String(), "mock-model") {
		t.Fatal(code, output.String())
	}
	output.Reset()
	rejectRegistry := func(agenttool.API) (*agenttool.Registry, error) {
		return nil, errors.New("private fixture diagnostics")
	}
	if code := runWith(context.Background(), nil, strings.NewReader(input), &output, factory, rejectRegistry); code != 2 || strings.Contains(output.String(), "private fixture") {
		t.Fatal(code, output.String())
	}
}
func TestCLIMainEntrypoint(t *testing.T) {
	oldArgs, oldOut, oldExit := os.Args, os.Stdout, exitProcess
	output, err := os.CreateTemp(t.TempDir(), "output")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { os.Args, os.Stdout, exitProcess = oldArgs, oldOut, oldExit; output.Close() }()
	os.Args = []string{"typesafe-tool", "--list"}
	os.Stdout = output
	code := -1
	exitProcess = func(value int) { code = value }
	main()
	if code != 0 {
		t.Fatal(code)
	}
	output.Seek(0, 0)
	raw, err := io.ReadAll(output)
	if err != nil || !strings.Contains(string(raw), "typesafe_evaluate") {
		t.Fatal("invalid stdout")
	}
}
