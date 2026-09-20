package agentpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
)

type fakeEvaluator struct {
	response *typesafe.SystemOneResponse
	err      error
	calls    int
}

func (f *fakeEvaluator) SystemOne(ctx context.Context, req typesafe.SystemOneRequest, opts ...typesafe.CallOption) (*typesafe.SystemOneResponse, error) {
	f.calls++
	return f.response, f.err
}

func fixture(t *testing.T, candidate string, confidence, write float64) *typesafe.SystemOneResponse {
	t.Helper()
	probabilities := map[string]float64{"explain": 0, "code_change": 0, "test_change": 0, "operations": 0, "unknown": 0}
	probabilities[candidate] = 1
	body := map[string]any{"model": "mock", "usage": map[string]any{}, "answers": map[string]any{
		"route":        map[string]any{"type": "choice", "choice": candidate, "probabilities": probabilities, "confidence": confidence},
		"write_intent": map[string]any{"type": "noul", "noul": write},
		"scope":        map[string]any{"type": "score", "score": 0, "probabilities": map[string]any{"0": 1, "1": 0, "2": 0}, "legend": map[string]any{"0": "local", "1": "component", "2": "cross"}, "confidence": 1},
	}}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var result typesafe.SystemOneResponse
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	result.HTTP = &typesafe.HTTPResponse{RequestID: "mock-request"}
	return &result
}

func TestConservativeRouting(t *testing.T) {
	cases := []struct {
		name, candidate   string
		confidence, write float64
		route, reason     string
		review            bool
	}{
		{"read_only", "explain", .9, .01, "explain", "read_only_advisory", false},
		{"low_confidence", "explain", .3, .01, "review", "low_confidence", true},
		{"writes", "explain", .9, .8, "review", "possible_state_change", true},
		{"boundary", "explain", .85, .2, "review", "possible_state_change", true},
		{"unknown", "unknown", .99, .01, "review", "unknown_intent", true},
		{"code", "code_change", .99, .01, "review", "human_review_required", true},
		{"tests", "test_change", .99, .01, "review", "human_review_required", true},
		{"ops", "operations", .99, .01, "review", "human_review_required", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeEvaluator{response: fixture(t, tc.candidate, tc.confidence, tc.write)}
			router, err := NewRouter(f, DefaultConfig())
			if err != nil {
				t.Fatal(err)
			}
			decision, err := router.Evaluate(context.Background(), Task{Text: "task"})
			if err != nil || decision.SuggestedRoute != tc.route || decision.RequiresReview != tc.review || decision.Reason != tc.reason || decision.RequestID != "mock-request" {
				t.Fatal(decision, err)
			}
		})
	}
}

func TestFailClosed(t *testing.T) {
	cases := []struct {
		name   string
		f      *fakeEvaluator
		reason string
	}{
		{"network_error", &fakeEvaluator{err: errors.New("network")}, "evaluation_failed"},
		{"nil_response", &fakeEvaluator{}, "invalid_response"},
		{"missing_answer", &fakeEvaluator{response: fixture(t, "explain", .99, .01)}, "invalid_response"},
	}
	delete(cases[2].f.response.Answers, "write_intent")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := NewRouter(tc.f, DefaultConfig())
			d, err := r.Evaluate(context.Background(), Task{Text: "task"})
			if err == nil || d.SuggestedRoute != "review" || !d.RequiresReview || d.Reason != tc.reason {
				t.Fatal(d, err)
			}
		})
	}
}

func TestInputAndConfigurationValidation(t *testing.T) {
	if _, err := NewRouter(nil, DefaultConfig()); err == nil {
		t.Fatal("nil evaluator")
	}
	for _, cfg := range []Config{{}, {Timeout: time.Second, MaxInputBytes: 10, MinRouteConfidence: math.NaN()}, {Timeout: time.Second, MaxInputBytes: 10, MaxWriteProbability: 2}} {
		if _, err := NewRouter(&fakeEvaluator{}, cfg); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	f := &fakeEvaluator{}
	r, _ := NewRouter(f, DefaultConfig())
	for _, task := range []Task{{}, {Text: string([]byte{0xff})}, {Text: strings.Repeat("x", 16001)}, {Text: "x", Summary: strings.Repeat("x", 16000)}} {
		d, err := r.Evaluate(context.Background(), task)
		if err == nil || !d.RequiresReview {
			t.Fatal("invalid task accepted")
		}
	}
	if _, err := r.Evaluate(nil, Task{Text: "task"}); err == nil {
		t.Fatal("nil context accepted")
	}
	if f.calls != 0 {
		t.Fatal("invalid input was transmitted")
	}
}
