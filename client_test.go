package typesafe

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func minimalRequest() SystemOneRequest {
	return SystemOneRequest{State: "hello", Questions: Questions{"q": Noul{Instructions: "Is this a greeting?"}}}
}

const minimalResponse = `{"model":"mock","answers":{"q":{"type":"noul","noul":0}},"usage":{}}`

func localClient(t *testing.T, handler http.HandlerFunc, change func(*Config)) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	cfg := Config{APIKey: "test-secret", BaseURL: s.URL, AllowInsecureHTTP: true, Retry: &RetryPolicy{}}
	if change != nil {
		change(&cfg)
	}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestSystemOneWire(t *testing.T) {
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/systemone" {
			t.Errorf("bad endpoint: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("authorization not protected")
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Accept") != "application/json" {
			t.Error("JSON headers")
		}
		if r.Header.Get("X-TypeSafe-SDK") != "typesafe-sdk-go/"+Version {
			t.Error("SDK identification")
		}
		if r.Header.Get("X-TypeSafe-Retry-Count") != "" {
			t.Error("initial retry count should be absent")
		}
		if r.Header.Get("X-Custom") != "per-call" {
			t.Error("per-call header precedence")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("body is not JSON")
		}
		if body["model"] != "request-model" {
			t.Error("request model override")
		}
		if body["state"] != "overridden" || body["experimental"] != true {
			t.Error("extra body merging")
		}
		w.Header().Set(RequestIDHeader, "req-123")
		_, _ = io.WriteString(w, minimalResponse)
	}, func(cfg *Config) {
		cfg.Headers = http.Header{"authorization": []string{"wrong"}, "X-Custom": []string{"default"}}
		cfg.Model = "client-model"
	})
	req := minimalRequest()
	req.Model = "request-model"
	req.ExtraBody = map[string]any{"state": "overridden", "experimental": true}
	result, err := c.SystemOne(context.Background(), req, WithHeaders(http.Header{"authorization": []string{"also-wrong"}, "X-Custom": []string{"per-call"}, "x-typesafe-retry-count": []string{"999"}}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Nouls["q"].Noul != 0 || result.HTTP.RequestID != "req-123" || result.HTTP.Attempts != 1 {
		t.Fatal("unexpected response", result)
	}
	if result.Usage.InputTokens != nil {
		t.Fatal("unreported tokens must remain nil")
	}
	if err := result.ValidateFor(req.Questions); err != nil {
		t.Fatal(err)
	}
}

func TestListModelsAndPrefix(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/gateway/v1/models" {
			t.Errorf("wrong endpoint %s %s", r.Method, r.URL.Path)
		}
		data, _ := io.ReadAll(r.Body)
		if len(data) != 0 {
			t.Error("GET has a body")
		}
		_, _ = io.WriteString(w, `{"models":[{"name":"mock","description":"test","release_date":"2026-09-20","future":true}]}`)
	}))
	defer s.Close()
	c, err := NewClient(Config{APIKey: "fake", BaseURL: s.URL + "/gateway/", AllowInsecureHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	models, err := c.Models().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models.Models) != 1 || models.Models[0].Name != "mock" || models.HTTP == nil {
		t.Fatal("model decode")
	}
}

func TestConfiguration(t *testing.T) {
	t.Setenv(APIKeyEnv, "  env-key  ")
	t.Setenv(BaseURLEnv, " https://example.test ")
	t.Setenv(DefaultModelEnv, " env-model ")
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.apiKey != "env-key" || c.baseURL != "https://example.test" || c.model != "env-model" {
		t.Fatal("environment resolution")
	}
	c2, err := NewClient(Config{APIKey: "explicit", Model: "explicit-model", BaseURL: "https://explicit.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if c2.apiKey != "explicit" || c2.model != "explicit-model" {
		t.Fatal("explicit precedence")
	}
	t.Setenv(APIKeyEnv, "")
	t.Setenv(BaseURLEnv, " ")
	t.Setenv(DefaultModelEnv, "")
	_, err = NewClient(Config{})
	if err == nil {
		t.Fatal("missing API key accepted")
	}
	cases := []Config{
		{APIKey: "x", BaseURL: "ftp://host"}, {APIKey: "x", BaseURL: "https://user:pass@host"},
		{APIKey: "x", BaseURL: "https://host?q=1"}, {APIKey: "x", BaseURL: "https://host#fragment"},
		{APIKey: "x", BaseURL: "http://host"}, {APIKey: "x", BaseURL: "https://host/v1"},
		{APIKey: "x", Timeout: -time.Second}, {APIKey: "x", MaxResponseBytes: -1},
		{APIKey: "x", MaxResponseBytes: 1 << 41}, {APIKey: "x", Model: " "}, {APIKey: "key\nvalue"},
		{APIKey: "x", Retry: &RetryPolicy{MaxRetries: -1}},
	}
	for i, cfg := range cases {
		if c, err := NewClient(cfg); err == nil {
			_ = c.Close()
			t.Errorf("invalid config %d accepted", i)
		}
	}
	borrowed := &http.Client{Timeout: 123 * time.Millisecond}
	c3, err := NewClient(Config{APIKey: "x", HTTPClient: borrowed})
	if err != nil {
		t.Fatal(err)
	}
	defer c3.Close()
	if c3.timeout != 123*time.Millisecond || borrowed.Timeout != 123*time.Millisecond {
		t.Fatal("borrowed timeout handling")
	}
}

func TestHTTPErrorClassification(t *testing.T) {
	for status, kind := range map[int]ErrorKind{400: KindBadRequest, 401: KindAuthentication, 403: KindPermissionDenied, 404: KindNotFound, 422: KindUnprocessableEntity, 429: KindRateLimit, 529: KindServer, 302: KindHTTP} {
		t.Run(string(kind), func(t *testing.T) {
			c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(RequestIDHeader, "request-id")
				w.Header().Set("Retry-After-Ms", "250")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":{"message":"private test-secret echo"}}`)
			}, nil)
			_, err := c.SystemOne(context.Background(), minimalRequest())
			var api *APIError
			if !errors.As(err, &api) || api.Kind != kind || api.StatusCode != status {
				t.Fatalf("wrong error: %v", err)
			}
			if api.RequestID != "request-id" || api.RetryAfter == nil || *api.RetryAfter != 250*time.Millisecond {
				t.Fatal("metadata missing")
			}
			if strings.Contains(err.Error(), "test-secret") || !strings.Contains(api.Message, "test-secret") {
				t.Fatal("safe error display / explicit message access")
			}
		})
	}
}

func TestHTTPRetryAndHeaders(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	events := []Event{}
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			if r.Header.Get("X-TypeSafe-Retry-Count") != "" {
				t.Error("first retry header")
			}
			w.Header().Set("Retry-After-Ms", "0")
			w.WriteHeader(529)
			return
		}
		if r.Header.Get("X-TypeSafe-Retry-Count") != "1" {
			t.Error("retry header missing")
		}
		_, _ = io.WriteString(w, minimalResponse)
	}, func(cfg *Config) {
		p := DefaultRetryPolicy()
		cfg.Retry = &p
		cfg.Observer = func(e Event) { mu.Lock(); events = append(events, e); mu.Unlock() }
	})
	r, err := c.SystemOne(context.Background(), minimalRequest())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || r.HTTP.Attempts != 2 {
		t.Fatal("retry count")
	}
	if len(events) != 2 || !events[0].WillRetry || events[1].StatusCode != 200 {
		t.Fatal("observer metadata")
	}
}

func TestBudgetDoesNotShortenRetryAfter(t *testing.T) {
	var calls atomic.Int32
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(429)
	}, func(cfg *Config) { p := DefaultRetryPolicy(); p.TotalTimeout = 100 * time.Millisecond; cfg.Retry = &p })
	start := time.Now()
	_, err := c.SystemOne(context.Background(), minimalRequest())
	var api *APIError
	if !errors.As(err, &api) || calls.Load() != 1 || time.Since(start) > time.Second {
		t.Fatal("budget ignored", err)
	}
}

func TestCancellationDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signaled := make(chan struct{}, 1)
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }, func(cfg *Config) {
		p := DefaultRetryPolicy()
		p.BackoffInitial = time.Second
		p.BackoffMax = time.Second
		p.BackoffJitter = 0
		cfg.Retry = &p
		cfg.Observer = func(e Event) {
			if e.WillRetry {
				signaled <- struct{}{}
			}
		}
	})
	done := make(chan error, 1)
	go func() { _, err := c.SystemOne(ctx, minimalRequest()); done <- err }()
	select {
	case <-signaled:
	case <-time.After(time.Second):
		t.Fatal("no retry")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation blocked")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportFailures(t *testing.T) {
	t.Run("connection_retried", func(t *testing.T) {
		var calls atomic.Int32
		boom := errors.New("sensitive underlying error")
		hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { calls.Add(1); return nil, boom })}
		p := DefaultRetryPolicy()
		p.BackoffInitial = 0
		c, err := NewClient(Config{APIKey: "x", HTTPClient: hc, Retry: &p})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_, err = c.SystemOne(context.Background(), minimalRequest())
		var ce *ConnectionError
		if !errors.As(err, &ce) || !errors.Is(err, boom) || calls.Load() != 3 {
			t.Fatal("transport retries", err, calls.Load())
		}
		if strings.Contains(err.Error(), "sensitive") {
			t.Fatal("error leaked data")
		}
	})
	t.Run("attempt_timeout", func(t *testing.T) {
		hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
		c, err := NewClient(Config{APIKey: "x", HTTPClient: hc, Retry: &RetryPolicy{}, Timeout: 10 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_, err = c.SystemOne(context.Background(), minimalRequest())
		var te *TimeoutError
		if !errors.As(err, &te) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("timeout classification", err)
		}
	})
	t.Run("parent_deadline", func(t *testing.T) {
		var calls atomic.Int32
		hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			<-r.Context().Done()
			return nil, r.Context().Err()
		})}
		c, err := NewClient(Config{APIKey: "x", HTTPClient: hc})
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, err = c.SystemOne(ctx, minimalRequest())
		if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
			t.Fatal("caller cancellation retried", err)
		}
	})
}

func TestRedirectAndSizeGuard(t *testing.T) {
	var hits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer destination.Close()
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}, nil)
	_, err := c.SystemOne(context.Background(), minimalRequest())
	var api *APIError
	if !errors.As(err, &api) || api.StatusCode != 307 || hits.Load() != 0 {
		t.Fatal("redirect followed", err)
	}
	big := localClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, strings.Repeat("x", 100)) }, func(cfg *Config) { cfg.MaxResponseBytes = 10 })
	_, err = big.SystemOne(context.Background(), minimalRequest())
	var large *ResponseTooLargeError
	if !errors.As(err, &large) {
		t.Fatal("body limit", err)
	}
}

func TestMalformedResponseNotRetried(t *testing.T) {
	var calls atomic.Int32
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"model":"mock","usage":{},"answers":{"q":{"type":"noul"}}}`)
	}, func(cfg *Config) { p := DefaultRetryPolicy(); cfg.Retry = &p })
	_, err := c.SystemOne(context.Background(), minimalRequest())
	var validation *ResponseValidationError
	if !errors.As(err, &validation) || validation.Field != "answers.q.noul" || validation.HTTP == nil || calls.Load() != 1 {
		t.Fatal("validation handling", err)
	}
}

func TestConcurrentClient(t *testing.T) {
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, minimalResponse) }, nil)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := c.SystemOne(context.Background(), minimalRequest())
			if err != nil || r == nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}

type customResult struct {
	OK *bool `json:"ok"`
}

func (c *customResult) Validate() error {
	if c.OK == nil {
		return errors.New("required ok")
	}
	return nil
}

func TestCustomResponseAndClosedClient(t *testing.T) {
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{"ok":true}`) }, nil)
	var dst customResult
	raw, err := c.SystemOneInto(context.Background(), minimalRequest(), &dst)
	if err != nil || dst.OK == nil || !*dst.OK || raw.StatusCode != 200 {
		t.Fatal("custom decoding", err)
	}
	for _, bad := range []any{nil, customResult{}, (*customResult)(nil)} {
		if _, err := c.SystemOneInto(context.Background(), minimalRequest(), bad); err == nil {
			t.Error("invalid destination accepted")
		}
	}
	if _, err := c.SystemOne(nil, minimalRequest()); err == nil {
		t.Error("nil context accepted")
	}
	if _, err := c.SystemOne(context.Background(), minimalRequest(), WithTimeout(0)); err == nil {
		t.Error("zero timeout accepted")
	}
	if _, err := c.SystemOne(context.Background(), minimalRequest(), nil); err == nil {
		t.Error("nil option accepted")
	}
	_ = c.Close()
	_ = c.Close()
	if _, err := c.ListModels(context.Background()); err == nil {
		t.Error("closed client accepted call")
	}
}

func TestCustomValidationFailure(t *testing.T) {
	c := localClient(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `{}`) }, nil)
	var dst customResult
	_, err := c.SystemOneInto(context.Background(), minimalRequest(), &dst)
	var validation *ResponseValidationError
	if !errors.As(err, &validation) {
		t.Fatal(err)
	}
}

func TestExtraBodyOverrideQuestions(t *testing.T) {
	req := minimalRequest()
	req.ExtraBody = map[string]any{"model": "new", "questions": map[string]any{"new": map[string]any{"type": "future"}}}
	data, err := prepareBody(req, "default")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(data, &body)
	want := map[string]any{"new": map[string]any{"type": "future"}}
	if body["model"] != "new" || !reflect.DeepEqual(body["questions"], want) {
		t.Fatal("not last-write-wins")
	}
}

func TestLiveAPI(t *testing.T) {
	if os.Getenv("TYPESAFE_LIVE_TEST") != "1" {
		t.Skip("opt in explicitly; live evaluation may incur charges")
	}
	if strings.TrimSpace(os.Getenv(APIKeyEnv)) == "" {
		t.Fatal("TYPESAFE_API_KEY is required for the opted-in live test")
	}
	c, err := NewClient(Config{Retry: &RetryPolicy{}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := c.ListModels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(models.Models) == 0 {
		t.Fatal("empty models")
	}
	req := minimalRequest()
	// A score question exercises the weighted-value consistency check, which a
	// noul-only request cannot reach.
	req.Questions["scope"] = ScoreLevels("How broad is the task?", "One local issue.", "One component.", "Several components.")
	result, err := c.SystemOne(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.ValidateFor(req.Questions); err != nil {
		t.Fatal(err)
	}
	if _, ok := result.Scores["scope"]; !ok {
		t.Fatal("score answer missing")
	}
}
