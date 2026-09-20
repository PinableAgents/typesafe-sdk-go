package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
	"github.com/PinableAgents/typesafe-sdk-go/contrib/agentpolicy"
	"github.com/PinableAgents/typesafe-sdk-go/internal/mockapi"
)

const mixedInput = `{"state":{"task":"Explain one Go function."},"questions":{"route":{"type":"choice","instructions":"Which activity?","criteria":{"explain":"Explain existing code.","change":"Change files."}},"scope":{"type":"score","instructions":"Work scope?","criteria":["One function.","One component."]},"write":{"type":"noul","instructions":"File changes requested?"}}}`

func local(t *testing.T, s *httptest.Server) (*Registry, *typesafe.Client) {
	t.Helper()
	t.Cleanup(s.Close)
	c, err := typesafe.NewClient(typesafe.Config{APIKey: "local-test-not-a-real-key", BaseURL: s.URL, AllowInsecureHTTP: true, Retry: &typesafe.RetryPolicy{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	r, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	return r, c
}

func TestDefinitions(t *testing.T) {
	definitions := Definitions()
	if len(definitions) != 3 {
		t.Fatal("missing tools")
	}
	for _, d := range definitions {
		if d.Name == "" || d.Description == "" || !json.Valid(d.InputSchema) {
			t.Fatal("invalid definition")
		}
	}
	definitions[0].InputSchema[0] = '!'
	if !json.Valid(Definitions()[0].InputSchema) {
		t.Fatal("shared mutable schema")
	}
}

func TestPublicToolCalls(t *testing.T) {
	r, _ := local(t, mockapi.New())
	ctx := context.Background()
	for _, tc := range []struct{ name, tool, input string }{
		{"models", ListModels, `{}`},
		{"mixed", Evaluate, mixedInput},
		{"route", RouteTask, `{"task":"Explain without changing files."}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Call(ctx, tc.tool, []byte(tc.input))
			if !got.OK || got.Error != nil {
				t.Fatal(got)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "local-test-not-a-real-key") || strings.Contains(string(b), "Authorization") || strings.Contains(string(b), `"Header"`) || strings.Contains(string(b), `"Body"`) {
				t.Fatal("raw HTTP metadata leaked")
			}
			if tc.tool == Evaluate && len(got.Data.(*typesafe.SystemOneResponse).Answers) != 3 {
				t.Fatal("missing typed answers")
			}
			if tc.tool == RouteTask && got.Data.(agentpolicy.Decision).SuggestedRoute != "explain" {
				t.Fatal("wrong fixture route")
			}
		})
	}
	got := r.Invoke(ctx, []byte(`{"tool":"typesafe_list_models","arguments":{}}`))
	if !got.OK {
		t.Fatal(got)
	}
}

func TestInvalidInputDoesNotSendHTTP(t *testing.T) {
	var calls atomic.Int32
	r, _ := local(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { calls.Add(1); w.WriteHeader(500) })))
	cases := []struct{ name, tool, input string }{
		{"null", ListModels, `null`}, {"array", ListModels, `[]`}, {"empty", ListModels, ``},
		{"trailing", ListModels, `{} {}`}, {"auth_override", ListModels, `{"api_key":"not-allowed"}`},
		{"missing_state", Evaluate, `{"questions":{"q":{"type":"noul"}}}`},
		{"bad_state", Evaluate, `{"state":42,"questions":{"q":{"type":"noul"}}}`},
		{"missing_questions", Evaluate, `{"state":"x"}`},
		{"empty_questions", Evaluate, `{"state":"x","questions":{}}`},
		{"bad_question", Evaluate, `{"state":"x","questions":{"q":null}}`},
		{"future_question", Evaluate, `{"state":"x","questions":{"q":{"type":"future"}}}`},
		{"bad_criteria", Evaluate, `{"state":"x","questions":{"q":{"type":"score","criteria":42}}}`},
		{"unknown_field", Evaluate, `{"state":"x","questions":{"q":{"type":"noul","url":"bad"}}}`},
		{"extra_body", Evaluate, `{"state":"x","questions":{"q":{"type":"noul"}},"extra_body":{"questions":{}}}`},
		{"empty_id", Evaluate, `{"state":"x","questions":{" ":{"type":"noul"}}}`},
		{"whitespace_task", RouteTask, `{"task":"  "}`},
		{"bad_summary", RouteTask, `{"task":"hello","context_summary":5}`},
		{"too_long", ListModels, strings.Repeat(" ", MaxArgumentBytes) + `{}`},
		{"invalid_utf8", ListModels, "{\"x\":\"\xff\"}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r.Call(context.Background(), tc.tool, []byte(tc.input))
			if got.OK || got.Error.Code != "invalid_arguments" || !got.Error.RequiresReview {
				t.Fatal(got)
			}
		})
	}
	q := map[string]any{}
	for i := 0; i < MaxQuestions+1; i++ {
		q[fmt.Sprint(i)] = map[string]any{"type": "noul"}
	}
	b, _ := json.Marshal(map[string]any{"state": "x", "questions": q})
	if r.Call(context.Background(), Evaluate, b).OK {
		t.Fatal("question cap ignored")
	}
	if r.Invoke(context.Background(), []byte(`{"tool":"typesafe_list_models","arguments":{},"headers":{}}`)).OK {
		t.Fatal("envelope fields ignored")
	}
	if calls.Load() != 0 {
		t.Fatalf("invalid inputs caused %d HTTP calls", calls.Load())
	}
}

type stub struct {
	response *typesafe.SystemOneResponse
	models   *typesafe.ListModelsResponse
	err      error
}

func (s *stub) SystemOne(context.Context, typesafe.SystemOneRequest, ...typesafe.CallOption) (*typesafe.SystemOneResponse, error) {
	return s.response, s.err
}
func (s *stub) ListModels(context.Context, ...typesafe.CallOption) (*typesafe.ListModelsResponse, error) {
	return s.models, s.err
}

func TestFailures(t *testing.T) {
	var nilClient *typesafe.Client
	for _, s := range []API{nil, nilClient, (*stub)(nil)} {
		if _, err := New(s); err == nil {
			t.Fatal("nil accepted")
		}
	}
	r, _ := New(&stub{})
	if r.Call(context.Background(), Evaluate, []byte(mixedInput)).Error.Code != "invalid_response" {
		t.Fatal("nil response accepted")
	}
	if r.Call(context.Background(), ListModels, []byte(`{}`)).OK {
		t.Fatal("empty models accepted")
	}
	if r.Call(nil, ListModels, []byte(`{}`)).OK {
		t.Fatal("nil context accepted")
	}
	if (*Registry)(nil).Call(context.Background(), ListModels, []byte(`{}`)).OK {
		t.Fatal("nil registry accepted")
	}
	if r.Call(context.Background(), "unknown", []byte(`{}`)).Error.Code != "unknown_tool" {
		t.Fatal("unknown tool accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r.Call(ctx, ListModels, []byte(`{}`)).Error.Code != "canceled" {
		t.Fatal("cancellation lost")
	}
	r, _ = New(&stub{err: &typesafe.ConnectionError{Cause: errors.New("sensitive payload")}})
	d := r.Call(context.Background(), RouteTask, []byte(`{"task":"Explain function."}`))
	if d.OK || !d.Data.(agentpolicy.Decision).RequiresReview || !d.Error.RequiresReview {
		t.Fatal("failure did not require review")
	}
	r, _ = New(&stub{response: &typesafe.SystemOneResponse{Model: "x", Answers: map[string]typesafe.Answer{}}})
	if r.Call(context.Background(), Evaluate, []byte(mixedInput)).OK {
		t.Fatal("missing answers accepted")
	}
}

func TestErrorSanitization(t *testing.T) {
	secret := "sensitive-canary-not-an-actual-key"
	cases := []struct {
		err  error
		code string
	}{
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "timeout"},
		{&typesafe.TimeoutError{}, "timeout"},
		{&typesafe.ConnectionError{Cause: errors.New(secret)}, "connection_failed"},
		{&typesafe.APIError{Kind: typesafe.KindAuthentication, StatusCode: 401, Message: secret, Body: []byte(secret), Header: http.Header{"Authorization": {secret}}}, "authentication"},
		{&typesafe.APIError{Kind: typesafe.KindRateLimit, StatusCode: 429}, "rate_limit"},
		{&typesafe.ValidationError{Field: secret, Message: secret}, "invalid_arguments"},
		{&typesafe.ResponseValidationError{Field: secret, Message: secret}, "invalid_response"},
		{&typesafe.ResponseTooLargeError{Limit: 42}, "response_too_large"},
		{errors.New(secret), "evaluation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			got := ErrorResult(tc.err)
			b, _ := json.Marshal(got)
			if got.OK || got.Error.Code != tc.code || !got.Error.RequiresReview || strings.Contains(string(b), secret) {
				t.Fatal("unsafe error result")
			}
			if got.Error.Retryable != (got.Error.HTTPStatus == 429) {
				t.Fatal("unexpected retry hint")
			}
		})
	}
}

func TestNoImplicitRetry(t *testing.T) {
	var calls atomic.Int32
	r, _ := local(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { calls.Add(1); w.WriteHeader(429) })))
	got := r.Call(context.Background(), Evaluate, []byte(mixedInput))
	if got.OK || got.Error.Code != "rate_limit" || calls.Load() != 1 {
		t.Fatal("implicit retry occurred")
	}
}

func TestConcurrentRegistry(t *testing.T) {
	r, _ := local(t, mockapi.New())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !r.Call(context.Background(), Evaluate, []byte(mixedInput)).OK {
				t.Error("concurrent call failed")
			}
		}()
	}
	wg.Wait()
}

func TestContextDeadline(t *testing.T) {
	r, _ := local(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { <-req.Context().Done() })))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if r.Call(ctx, ListModels, []byte(`{}`)).Error.Code != "timeout" {
		t.Fatal("parent deadline ignored")
	}
}

func TestStructuredNumbersRemainExact(t *testing.T) {
	r, _ := local(t, httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(req.Body).Decode(&body)
		if !strings.Contains(string(body["questions"]), "9007199254740993") {
			t.Error("JSON number rounded")
		}
		w.WriteHeader(401)
	})))
	r.Call(context.Background(), Evaluate, []byte(`{"state":"x","questions":{"q":{"type":"noul","instructions":{"id":9007199254740993}}}}`))
}
