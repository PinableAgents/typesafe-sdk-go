package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
	"github.com/PinableAgents/typesafe-sdk-go/contrib/agentpolicy"
	"github.com/PinableAgents/typesafe-sdk-go/internal/mockapi"
)

func TestMetadataWhitelistsDiagnosticFields(t *testing.T) {
	raw := &typesafe.HTTPResponse{Protocol: "HTTP/2.0", RequestID: "req-fixture", Duration: 23 * time.Millisecond, Attempts: 2, Header: http.Header{"Authorization": []string{"private-header"}}, Body: []byte("private-body")}
	meta := responseMetadata("fixture-model", raw)
	encoded, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	s := string(encoded)
	for _, want := range []string{`"request_id":"req-fixture"`, `"attempts":2`, `"duration_ms":23`, `"protocol":"HTTP/2.0"`, `"model":"fixture-model"`} {
		if !strings.Contains(s, want) {
			t.Fatal(s)
		}
	}
	for _, forbidden := range []string{"private", "Authorization", "Header", "Body"} {
		if strings.Contains(s, forbidden) {
			t.Fatal("metadata leaked raw HTTP")
		}
	}
	if responseMetadata("model", nil).Model != "model" {
		t.Fatal("missing HTTP metadata mishandled")
	}
}

func TestToolsExposeMetadata(t *testing.T) {
	r, _ := local(t, mockapi.New())
	for _, tc := range []struct{ tool, input string }{{ListModels, `{}`}, {Evaluate, mixedInput}, {RouteTask, `{"task":"Explain a function."}`}} {
		out := r.Call(context.Background(), tc.tool, []byte(tc.input))
		if !out.OK || out.Metadata == nil {
			t.Fatal(out)
		}
		if out.Metadata.Attempts != 1 {
			t.Fatal("missing attempts")
		}
		if tc.tool == RouteTask && out.Metadata.RubricVersion != agentpolicy.RubricVersion {
			t.Fatal("missing rubric")
		}
		if tc.tool == Evaluate && (out.Metadata.Model == "" || out.Metadata.Protocol != "HTTP/1.1") {
			t.Fatal("missing model/protocol")
		}
	}
}

type rejectingAPI struct{ calls int }

func (s *rejectingAPI) SystemOne(context.Context, typesafe.SystemOneRequest, ...typesafe.CallOption) (*typesafe.SystemOneResponse, error) {
	s.calls++
	return nil, errors.New("unexpected API call")
}
func (s *rejectingAPI) ListModels(context.Context, ...typesafe.CallOption) (*typesafe.ListModelsResponse, error) {
	s.calls++
	return nil, errors.New("unexpected API call")
}
func TestToolValidatesBeforeCustomAPI(t *testing.T) {
	api := &rejectingAPI{}
	r, err := New(api)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`{"type":"noul"}`, `{"type":"noul","criteria":{"true":null,"false":null}}`, `{"type":"choice","criteria":{}}`} {
		input := `{"state":"fixture","questions":{"q":` + q + `}}`
		out := r.Call(context.Background(), Evaluate, []byte(input))
		if out.OK || out.Error.Code != "invalid_arguments" || !out.Error.RequiresReview {
			t.Fatal(out)
		}
	}
	if api.calls != 0 {
		t.Fatal("custom API received invalid input")
	}
}
