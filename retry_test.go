package typesafe

import (
	"errors"
	"math"
	"net/http"
	"testing"
	"time"
)

func TestRetryAfterParsing(t *testing.T) {
	now := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		headers http.Header
		want    time.Duration
		valid   bool
	}{
		{"ms_first", http.Header{"Retry-After-Ms": []string{"250"}, "Retry-After": []string{"2"}}, 250 * time.Millisecond, true},
		{"decimal_seconds", http.Header{"Retry-After": []string{"1.5"}}, 1500 * time.Millisecond, true},
		{"date", http.Header{"Retry-After": []string{now.Add(3 * time.Second).Format(http.TimeFormat)}}, 3 * time.Second, true},
		{"past_date", http.Header{"Retry-After": []string{now.Add(-time.Second).Format(http.TimeFormat)}}, 0, true},
		{"invalid_ms_fallback", http.Header{"Retry-After-Ms": []string{"NaN"}, "Retry-After": []string{"2"}}, 2 * time.Second, true},
		{"negative_ms_fallback", http.Header{"Retry-After-Ms": []string{"-1"}, "Retry-After": []string{"2"}}, 2 * time.Second, true},
		{"blank", http.Header{"Retry-After": []string{" "}}, 0, true},
		{"negative_seconds", http.Header{"Retry-After": []string{"-1"}}, 0, false},
		{"invalid", http.Header{"Retry-After": []string{"tomorrow"}}, 0, false},
		{"empty", http.Header{}, 0, false},
		{"huge", http.Header{"Retry-After": []string{"1e300"}}, time.Duration(math.MaxInt64), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseRetryAfter(tc.headers, now)
			if tc.valid {
				if got == nil || *got != tc.want {
					t.Fatalf("got %v want %v", got, tc.want)
				}
			} else if got != nil {
				t.Fatal("invalid accepted", *got)
			}
		})
	}
}

func TestRetryPolicyValidationAndBackoff(t *testing.T) {
	for _, p := range []RetryPolicy{{MaxRetries: -1}, {BackoffInitial: -1}, {BackoffMax: -1}, {TotalTimeout: -1}, {BackoffJitter: math.NaN()}, {BackoffJitter: 1.1}, {HTTPStatuses: []int{999}}} {
		if p.validate() == nil {
			t.Error("invalid policy accepted")
		}
	}
	p := DefaultRetryPolicy()
	p.BackoffJitter = 0
	for attempt, want := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second} {
		if got := p.delay(attempt, errors.New("test")); got != want {
			t.Errorf("delay %d=%s, want %s", attempt, got, want)
		}
	}
	p.BackoffJitter = .25
	for i := 0; i < 100; i++ {
		d := p.delay(0, nil)
		if d < 375*time.Millisecond || d > 500*time.Millisecond {
			t.Fatal("jitter out of range")
		}
	}
	boom := errors.New("extra failure")
	p = RetryPolicy{Predicate: func(err error) bool { return errors.Is(err, boom) }}
	if !p.retryable(boom) || p.retryable(&APIError{StatusCode: 401}) {
		t.Fatal("custom predicate")
	}
	zero := RetryPolicy{}
	if zero.retryable(&ConnectionError{}) || zero.retryable(&TimeoutError{}) || zero.delay(0, nil) != 0 {
		t.Fatal("zero policy must disable built-ins")
	}
}

func TestErrorMessageExtraction(t *testing.T) {
	cases := map[string]string{
		`"plain"`: "plain", `not-json`: "not-json", `{"error":"oops"}`: "oops", `{"message":"oops"}`: "oops",
		`{"detail":"oops"}`: "oops", `{"detail":{"message":"oops"}}`: "oops",
		`{"detail":[{"loc":["body","questions","q"],"msg":"bad"}]}`: "questions.q: bad",
		`{"x":1}`: "", `[]`: "",
	}
	for input, want := range cases {
		if got := extractMessage([]byte(input)); got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}
