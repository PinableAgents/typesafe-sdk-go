package main

import (
	"flag"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
)

// Never uses real credentials or the public service, including the no--mock paths.
func setupExample(t *testing.T, args ...string) {
	t.Helper()
	oldArgs, oldFlags, oldOut, oldErr, oldExit := os.Args, flag.CommandLine, os.Stdout, os.Stderr, exitProcess
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	diagnostics, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	os.Args = append([]string{"example"}, args...)
	flag.CommandLine = flag.NewFlagSet("example", flag.ContinueOnError)
	flag.CommandLine.SetOutput(diagnostics)
	os.Stdout, os.Stderr = out, diagnostics
	t.Setenv(typesafe.APIKeyEnv, "")
	t.Setenv(typesafe.BaseURLEnv, "")
	t.Setenv(typesafe.DefaultModelEnv, "")
	t.Cleanup(func() {
		os.Args, flag.CommandLine, os.Stdout, os.Stderr, exitProcess = oldArgs, oldFlags, oldOut, oldErr, oldExit
		out.Close()
		diagnostics.Close()
	})
}

type exampleTransport func(*http.Request) (*http.Response, error)

func (f exampleTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func installResponse(t *testing.T, status int, body string) {
	t.Helper()
	old := http.DefaultTransport
	http.DefaultTransport = exampleTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("unexpected credential")
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = old })
	t.Setenv(typesafe.APIKeyEnv, "fixture-key")
}

func TestExampleMain(t *testing.T) {
	t.Run("mock_success", func(t *testing.T) {
		setupExample(t, "--mock")
		exitProcess = func(code int) { t.Fatalf("unexpected exit %d", code) }
		main()
		if _, err := os.Stdout.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(os.Stdout)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"mock-model"`) {
			t.Fatalf("unexpected output %s", data)
		}
	})
	t.Run("configuration_failure", func(t *testing.T) {
		setupExample(t)
		code := -1
		exitProcess = func(value int) { code = value }
		main()
		if code != 1 {
			t.Fatal("error did not exit with 1", code)
		}
	})
}
func TestExampleHTTPFailure(t *testing.T) {
	setupExample(t)
	installResponse(t, 401, `{"error":"fixture rejected"}`)
	if err := run(); err == nil {
		t.Fatal("HTTP error hidden")
	}
}
func TestExampleOutputFailure(t *testing.T) {
	setupExample(t, "--mock")
	os.Stdout.Close()
	if err := run(); err == nil {
		t.Fatal("output error hidden")
	}
}
func TestExampleInvalidContract(t *testing.T) {
	setupExample(t)
	installResponse(t, 200, `{"model":"fixture","usage":{},"answers":{}}`)
	if err := run(); err == nil {
		t.Fatal("missing answers accepted")
	}
}
