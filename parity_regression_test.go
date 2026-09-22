package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"
)

// These cases implement the documented Python v0.7.1 authentication contract.
// All keys below are artificial; no network call reaches a real service.
func TestDocumentedAPIKeyValidation(t *testing.T) {
	t.Setenv(APIKeyEnv, "environment-fixture")
	for _, key := range []string{"a b", "a\tb", "a\nb", "a\rb", "a\x00b", "a\x1fb", "a\x7fb", "密钥", "a\u00a0b"} {
		t.Run(strings.ReplaceAll(key, "/", "_"), func(t *testing.T) {
			_, err := NewClient(Config{APIKey: key})
			var invalid *ValidationError
			if !errors.As(err, &invalid) || invalid.Field != "api_key" {
				t.Fatalf("expected local validation: %T", err)
			}
			if strings.Contains(err.Error(), key) {
				t.Fatal("key leaked in error")
			}
		})
	}
	for _, key := range []string{"", " \n\t "} {
		if _, err := NewClient(Config{APIKey: key, APIKeySet: true}); err == nil {
			t.Fatal("explicit empty key inherited environment")
		}
	}
	c, err := NewClient(Config{APIKey: " \t fixture-key \r\n"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.apiKey != "fixture-key" {
		t.Fatal("surrounding whitespace was not stripped")
	}
	inherited, err := NewClient(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer inherited.Close()
	if inherited.apiKey != "environment-fixture" {
		t.Fatal("omitted key did not inherit environment")
	}
}

type trackingTransport struct {
	closed int
	calls  int
}

func (tr *trackingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	tr.calls++
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"models":[]}`))}, nil
}
func (tr *trackingTransport) CloseIdleConnections() { tr.closed++ }

func TestTransportOwnershipAndMutualExclusion(t *testing.T) {
	tr := &trackingTransport{}
	if _, err := NewClient(Config{APIKey: "fixture", Transport: tr, HTTPClient: &http.Client{}}); err == nil {
		t.Fatal("conflicting transport accepted")
	}
	if _, err := NewClient(Config{APIKey: "fixture", HTTPClient: &http.Client{Timeout: -time.Second}}); err == nil {
		t.Fatal("negative inherited timeout accepted")
	}
	c, err := NewClient(Config{APIKey: "fixture", Transport: tr})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.Close()
	c.Close()
	if tr.closed != 1 || tr.calls != 1 {
		t.Fatal("owned transport not closed exactly once")
	}
	borrowed, err := NewClient(Config{APIKey: "fixture", HTTPClient: &http.Client{Transport: tr}})
	if err != nil {
		t.Fatal(err)
	}
	borrowed.Close()
	if tr.closed != 1 {
		t.Fatal("borrowed transport closed")
	}
	original := http.DefaultTransport
	http.DefaultTransport = tr
	t.Cleanup(func() { http.DefaultTransport = original })
	shared, err := NewClient(Config{APIKey: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	shared.Close()
	if shared.ownsHTTP || tr.closed != 1 {
		t.Fatal("shared global transport closed")
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenReader) Close() error             { return nil }

func TestAdditionalClientFailures(t *testing.T) {
	c, err := NewClient(Config{APIKey: "fixture", Retry: &RetryPolicy{}, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: brokenReader{}}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, err = c.ListModels(context.Background())
	var conn *ConnectionError
	if !errors.As(err, &conn) || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("body read error lost", err)
	}
	if _, err = c.ListModels(context.Background(), WithRetry(RetryPolicy{MaxRetries: -1})); err == nil {
		t.Fatal("invalid override accepted")
	}
	var dst map[string]any
	if _, err = c.SystemOneInto(context.Background(), SystemOneRequest{}, &dst); err == nil {
		t.Fatal("invalid custom request accepted")
	}
	opt, err := c.callOptions(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.attempt(context.Background(), "bad method", "/v1/models", nil, &dst, opt, 0); err == nil {
		t.Fatal("bad method accepted")
	}
	// Custom destinations use the same sanitized response validation envelope.
	c2 := localClient(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"value":"not-an-int"}`) }, nil)
	var typed struct {
		Value int `json:"value"`
	}
	_, err = c2.SystemOneInto(context.Background(), minimalRequest(), &typed)
	var invalid *ResponseValidationError
	if !errors.As(err, &invalid) || invalid.HTTP == nil {
		t.Fatal("custom decode failure lost", err)
	}
}

func TestRetryRespectsCallerSchedulingDeadline(t *testing.T) {
	calls := 0
	c, err := NewClient(Config{APIKey: "fixture", Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{"3600"}}, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := DefaultRetryPolicy()
	p.TotalTimeout = 0
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	_, err = c.ListModels(ctx, WithRetry(p))
	var api *APIError
	if !errors.As(err, &api) || calls != 1 {
		t.Fatal("retry crossed caller deadline", err, calls)
	}
}

func TestAdditionalErrorAndParsingContracts(t *testing.T) {
	cause := errors.New("private fixture value")
	for _, err := range []error{&ValidationError{Field: "state", Message: "invalid"}, &ResponseValidationError{Field: "answers", Message: "invalid"}, &TimeoutError{Cause: cause, Timeout: time.Second}, &ResponseTooLargeError{Limit: 10}} {
		if err.Error() == "" || strings.Contains(err.Error(), cause.Error()) {
			t.Fatal("unsafe error formatting")
		}
	}
	if errorCategory(context.Canceled) != "canceled" {
		t.Fatal("cancellation category")
	}
	if contentOK(nil, false) || contentOK([]byte("  "), true) {
		t.Fatal("empty JSON accepted")
	}
	if _, err := decodeLegendContent([]byte(`{`)); err == nil {
		t.Fatal("malformed legend accepted")
	}
	if _, err := decodeLegendContent([]byte(`false`)); err == nil {
		t.Fatal("scalar legend accepted")
	}
	value, err := decodeLegendContent([]byte(`{"id":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if value.(map[string]any)["id"] != json.Number("9007199254740993") {
		t.Fatal("number precision lost")
	}
	for input, want := range map[string]string{
		`{"detail":[4,{"msg":3},{"msg":"bad"},{"loc":["body",2],"msg":"wrong"}]}`: "bad; 2: wrong",
	} {
		if got := extractMessage([]byte(input)); got != want {
			t.Fatalf("%q != %q", got, want)
		}
	}
	p := RetryPolicy{BackoffInitial: 2 * time.Second, BackoffMax: time.Second}
	if d := p.delay(0, nil); d != time.Second {
		t.Fatal("initial backoff not capped", d)
	}
}

type futureAnswer struct{}

func (futureAnswer) AnswerType() string { return "future" }
func TestAdditionalResponseContractFailures(t *testing.T) {
	r := &SystemOneResponse{}
	if err := r.ValidateFor(nil); err == nil {
		t.Fatal("empty questions accepted")
	}
	r.Answers = map[string]Answer{"q": futureAnswer{}}
	if err := r.ValidateFor(Questions{"q": RawQuestion{"type": "future"}}); err == nil {
		t.Fatal("unknown kind authorized")
	}
	for _, v := range []float64{-0.0001, 1.0001, math.NaN(), math.Inf(1)} {
		probs := map[int]float64{0: 1, 1: 0}
		if v > 0 {
			probs = map[int]float64{0: 0, 1: 1}
		}
		score := ScoreAnswer{Type: "score", Score: v, Confidence: 1, Legend: map[int]any{0: "a", 1: "b"}, Probabilities: probs}
		r.Answers = map[string]Answer{"q": score}
		r.Scores = map[string]ScoreAnswer{"q": score}
		if err := r.ValidateFor(Questions{"q": ScoreLevels(nil, "a", "b")}); err == nil {
			t.Fatal("out-of-domain score accepted", v)
		}
	}
}
