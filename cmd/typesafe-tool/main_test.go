package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PinableAgents/typesafe-sdk-go/contrib/agenttool"
)

func TestListWithoutKey(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	var out bytes.Buffer
	if run(context.Background(), []string{"--list"}, strings.NewReader(""), &out) != 0 {
		t.Fatal("list requires credentials")
	}
	var parsed struct {
		Tools []agenttool.Definition `json:"tools"`
	}
	if json.Unmarshal(out.Bytes(), &parsed) != nil || len(parsed.Tools) != 3 {
		t.Fatal("not tool JSON")
	}
}

func TestInvalidCLI(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	for _, tc := range []struct {
		name string
		args []string
		in   string
		code string
	}{
		{"flags", []string{"--api-key", "must-not-echo"}, "", "usage"},
		{"bad_json", nil, "not JSON", "invalid_arguments"},
		{"oversize", nil, strings.Repeat(" ", agenttool.MaxArgumentBytes+1), "invalid_arguments"},
		{"no_key", nil, `{"tool":"typesafe_list_models","arguments":{}}`, "configuration_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if run(context.Background(), tc.args, strings.NewReader(tc.in), &out) == 0 {
				t.Fatal("expected failure exit")
			}
			var got agenttool.Result
			if json.Unmarshal(out.Bytes(), &got) != nil || got.OK || got.Error.Code != tc.code {
				t.Fatal("not structured failure")
			}
			if strings.Contains(out.String(), "must-not-echo") {
				t.Fatal("CLI arguments leaked")
			}
		})
	}
}

func TestCanceledCLI(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "local-config-canceled-before-http")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if run(ctx, nil, strings.NewReader(`{"tool":"typesafe_list_models","arguments":{}}`), &out) != 1 {
		t.Fatal("canceled call accepted")
	}
	if !strings.Contains(out.String(), `"code":"canceled"`) {
		t.Fatal(out.String())
	}
}
