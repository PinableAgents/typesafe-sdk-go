package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"testing"
)

type behaviorCase struct {
	Name           string         `json:"name"`
	Question       map[string]any `json:"question"`
	Typed          bool           `json:"typed"`
	State          any            `json:"state"`
	ExtraBody      map[string]any `json:"extra_body"`
	Statuses       []int          `json:"statuses"`
	Retries        int            `json:"retries"`
	TransportError string         `json:"transport_error"`
	Error          string         `json:"error"`
	Calls          int            `json:"calls"`
}

// Test-only fixture expansion. Production callers explicitly select compatible
// float fields; the SDK never rewrites arbitrary objects or sentinel maps.
func behaviorInput(v any) any {
	switch x := v.(type) {
	case map[string]any:
		if token, ok := x["$nonfinite"].(string); ok {
			switch token {
			case "NaN":
				return NonFiniteFloat64(math.NaN())
			case "Infinity":
				return NonFiniteFloat64(math.Inf(1))
			case "-Infinity":
				return NonFiniteFloat64(math.Inf(-1))
			}
		}
		m := map[string]any{}
		for k, v := range x {
			m[k] = behaviorInput(v)
		}
		return m
	case []any:
		a := make([]any, len(x))
		for i, v := range x {
			a[i] = behaviorInput(v)
		}
		return a
	default:
		return v
	}
}
func TestPython074BehaviorCorpus(t *testing.T) {
	data, err := os.ReadFile("testdata/python074-behavior.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []behaviorCase
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if dec.Decode(&cases) != nil {
		t.Fatal("invalid corpus")
	}
	results := map[string]any{}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			calls := 0
			counts := []string{}
			var wire any
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			retry := DefaultRetryPolicy()
			retry.MaxRetries = tc.Retries
			retry.BackoffInitial = 0
			retry.BackoffMax = 0
			tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				counts = append(counts, r.Header.Get("X-TypeSafe-Retry-Count"))
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatal(err)
				}
				wire = contractJSON(t, raw)
				switch tc.TransportError {
				case "connection":
					return nil, errors.New("synthetic connection failure")
				case "timeout":
					return nil, context.DeadlineExceeded
				case "canceled":
					cancel()
					return nil, context.Canceled
				}
				code := http.StatusOK
				if len(tc.Statuses) > 0 {
					code = tc.Statuses[min(calls-1, len(tc.Statuses)-1)]
				}
				body := minimalResponse
				if code != 200 {
					body = `{"message":"fixture error"}`
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Retry-After-Ms": []string{"0"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			c, err := NewClient(Config{APIKey: "fixture-key", Transport: tr, Retry: &retry})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			q := behaviorInput(tc.Question).(map[string]any)
			var question Question = RawQuestion(q)
			if tc.Typed {
				switch q["type"] {
				case "choice":
					question = Choice{Instructions: q["instructions"], Criteria: q["criteria"].(map[string]any)}
				case "score":
					question = Score{Instructions: q["instructions"], Criteria: q["criteria"].([]any)}
				case "noul":
					var criteria NoulCriteria
					if q["criteria"] != nil {
						criteria = NoulCriteria(q["criteria"].(map[string]any))
					}
					question = Noul{Instructions: q["instructions"], Criteria: criteria}
				}
			}
			state := tc.State
			if state == nil {
				state = "fixture state"
			}
			var extra map[string]any
			if tc.ExtraBody != nil {
				extra = behaviorInput(tc.ExtraBody).(map[string]any)
			}
			_, err = c.SystemOne(ctx, SystemOneRequest{State: behaviorInput(state), Questions: Questions{"q": question}, ExtraBody: extra})
			category := ""
			var api *APIError
			var invalid *ValidationError
			var timeout *TimeoutError
			switch {
			case err == nil:
			case errors.Is(err, context.Canceled):
				category = "canceled"
			case errors.As(err, &invalid):
				category = "invalid_request"
			case errors.As(err, &api):
				category = fmt.Sprintf("http_%d", api.StatusCode)
			case errors.As(err, &timeout):
				category = "timeout"
			default:
				category = "connection"
			}
			if category != tc.Error || calls != tc.Calls {
				t.Fatalf("error=%s calls=%d want %s/%d (%v)", category, calls, tc.Error, tc.Calls, err)
			}
			results[tc.Name] = map[string]any{"calls": calls, "error": category, "request": wire, "retry_counts": counts}
		})
	}
	if path := os.Getenv("TYPESAFE_BEHAVIOR_OUTPUT"); path != "" {
		raw, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
