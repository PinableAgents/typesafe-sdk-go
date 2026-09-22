package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Event contains metadata only. Observer is called synchronously and may be
// called concurrently by shared clients. It must be fast and concurrency-safe.
type Event struct {
	Method     string
	Path       string
	Attempt    int
	StatusCode int
	RequestID  string
	Duration   time.Duration
	RetryDelay time.Duration
	WillRetry  bool
	ErrorKind  string
}

type Config struct {
	APIKey string
	// APIKeySet distinguishes an explicitly empty key from an omitted key.
	// Leave false to preserve the existing empty-string/environment behavior.
	APIKeySet  bool
	BaseURL    string
	Model      string
	Timeout    time.Duration
	Retry      *RetryPolicy
	Headers    http.Header
	HTTPClient *http.Client
	// Transport is mutually exclusive with HTTPClient. Idle connections on a
	// supplied transport are closed by Close; borrowed HTTPClient stays borrowed.
	Transport         http.RoundTripper
	MaxResponseBytes  int64
	AllowInsecureHTTP bool
	Observer          func(Event)
}

type Client struct {
	apiKey           string
	baseURL          string
	model            string
	timeout          time.Duration
	retry            RetryPolicy
	headers          http.Header
	httpClient       *http.Client
	ownsHTTP         bool
	maxResponseBytes int64
	observer         func(Event)
	closed           atomic.Bool
}

func envDefault(explicit, name, fallback string) string {
	if explicit != "" {
		return explicit
	}
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

// NewClient resolves config, then environment, then defaults. An empty string
// config field inherits its environment unless APIKeySet is true. It makes no
// network requests.
func NewClient(cfg Config) (*Client, error) {
	key := cfg.APIKey
	if !cfg.APIKeySet {
		key = envDefault(key, APIKeyEnv, "")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, invalid("api_key", "set TYPESAFE_API_KEY or Config.APIKey")
	}
	for _, c := range key {
		if c < 33 || c > 126 {
			return nil, invalid("api_key", "must contain only printable ASCII without internal whitespace")
		}
	}
	if cfg.HTTPClient != nil && cfg.Transport != nil {
		return nil, invalid("transport", "Transport and HTTPClient are mutually exclusive")
	}
	base := strings.TrimRight(envDefault(cfg.BaseURL, BaseURLEnv, DefaultBaseURL), "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, invalid("base_url", "use an http(s) API root without credentials, query, or fragment")
	}
	if u.Scheme == "http" && !cfg.AllowInsecureHTTP {
		return nil, invalid("base_url", "HTTP requires explicit AllowInsecureHTTP=true; use HTTPS for real credentials")
	}
	if strings.HasSuffix(u.Path, "/v1") {
		return nil, invalid("base_url", "use the API root, without the /v1 suffix")
	}
	timeout := cfg.Timeout
	if timeout < 0 {
		return nil, invalid("timeout", "must not be negative")
	}
	if timeout == 0 && cfg.HTTPClient != nil {
		timeout = cfg.HTTPClient.Timeout
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout < 0 {
		return nil, invalid("timeout", "must be positive")
	}
	model := envDefault(cfg.Model, DefaultModelEnv, DefaultModel)
	if strings.TrimSpace(model) == "" {
		return nil, invalid("model", "must not be blank")
	}
	retry := DefaultRetryPolicy()
	if cfg.Retry != nil {
		retry = cloneRetry(*cfg.Retry)
	}
	if err := retry.validate(); err != nil {
		return nil, err
	}
	limit := cfg.MaxResponseBytes
	if limit == 0 {
		limit = defaultMaxResponseBytes
	}
	if limit < 1 || limit > 1<<40 {
		return nil, invalid("max_response_bytes", "must be between 1 byte and 1 TiB")
	}
	var hc http.Client
	owns := cfg.HTTPClient == nil
	if cfg.HTTPClient != nil {
		hc = *cfg.HTTPClient
	} else if cfg.Transport != nil {
		hc.Transport = cfg.Transport
	} else if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		hc.Transport = tr.Clone()
	} else {
		hc.Transport = http.DefaultTransport
		owns = false // Do not close an application's shared global transport.
	}
	// Per-attempt contexts implement SDK timeout overrides. Do not mutate a
	// caller's http.Client, and never forward Authorization across redirects.
	hc.Timeout = 0
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{apiKey: key, baseURL: base, model: model, timeout: timeout, retry: retry,
		headers: cfg.Headers.Clone(), httpClient: &hc, ownsHTTP: owns, maxResponseBytes: limit, observer: cfg.Observer}, nil
}

// Close prevents new calls and closes idle connections only for SDK-owned HTTP
// clients. It does not cancel calls already in progress. Cancel their contexts.
func (c *Client) Close() error {
	if c.closed.CompareAndSwap(false, true) && c.ownsHTTP {
		c.httpClient.CloseIdleConnections()
	}
	return nil
}

type callConfig struct {
	timeout time.Duration
	retry   RetryPolicy
	headers http.Header
}
type CallOption func(*callConfig) error

func WithTimeout(timeout time.Duration) CallOption {
	return func(o *callConfig) error {
		if timeout <= 0 {
			return invalid("timeout", "must be positive")
		}
		o.timeout = timeout
		return nil
	}
}
func WithRetry(policy RetryPolicy) CallOption {
	p := cloneRetry(policy)
	return func(o *callConfig) error {
		if err := p.validate(); err != nil {
			return err
		}
		o.retry = cloneRetry(p)
		return nil
	}
}
func WithHeaders(headers http.Header) CallOption {
	snapshot := headers.Clone()
	return func(o *callConfig) error {
		for k, values := range snapshot {
			o.headers[http.CanonicalHeaderKey(k)] = append([]string(nil), values...)
		}
		return nil
	}
}

func (c *Client) callOptions(opts []CallOption) (callConfig, error) {
	o := callConfig{timeout: c.timeout, retry: cloneRetry(c.retry), headers: make(http.Header)}
	// Recanonicalize manually supplied maps so protected lower-case headers
	// cannot survive alongside the canonical protected headers.
	for k, v := range c.headers {
		o.headers[http.CanonicalHeaderKey(k)] = append([]string(nil), v...)
	}
	for _, opt := range opts {
		if opt == nil {
			return o, invalid("options", "nil call option")
		}
		if err := opt(&o); err != nil {
			return o, err
		}
	}
	o.headers.Del("X-TypeSafe-Retry-Count")
	o.headers.Set("Authorization", "Bearer "+c.apiKey)
	o.headers.Set("Accept", "application/json")
	o.headers.Set("User-Agent", "typesafe-sdk-go/"+Version)
	o.headers.Set("X-TypeSafe-SDK", "typesafe-sdk-go/"+Version)
	o.headers.Set("X-TypeSafe-Runtime", fmt.Sprintf("%s (%s; %s)", runtime.Version(), runtime.GOOS, runtime.GOARCH))
	return o, nil
}

func (c *Client) SystemOne(ctx context.Context, req SystemOneRequest, opts ...CallOption) (*SystemOneResponse, error) {
	body, err := prepareBody(req, c.model)
	if err != nil {
		return nil, err
	}
	var result SystemOneResponse
	raw, err := c.request(ctx, http.MethodPost, "/v1/systemone", body, &result, opts)
	if err != nil {
		return nil, err
	}
	result.HTTP = raw
	return &result, nil
}

// SystemOneInto decodes into a non-nil pointer supplied by the caller. Go's JSON
// decoder does NOT enforce required fields. Implement Validate() error on dst
// for additional validation. This is an advanced alternative to SystemOne.
func (c *Client) SystemOneInto(ctx context.Context, req SystemOneRequest, dst any, opts ...CallOption) (*HTTPResponse, error) {
	if dst == nil {
		return nil, invalid("response", "destination must be a non-nil pointer")
	}
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return nil, invalid("response", "destination must be a non-nil pointer")
	}
	body, err := prepareBody(req, c.model)
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodPost, "/v1/systemone", body, dst, opts)
}

func (c *Client) ListModels(ctx context.Context, opts ...CallOption) (*ListModelsResponse, error) {
	var result ListModelsResponse
	raw, err := c.request(ctx, http.MethodGet, "/v1/models", nil, &result, opts)
	if err != nil {
		return nil, err
	}
	result.HTTP = raw
	return &result, nil
}

type ModelsService struct{ client *Client }

func (c *Client) Models() ModelsService { return ModelsService{client: c} }
func (s ModelsService) List(ctx context.Context, opts ...CallOption) (*ListModelsResponse, error) {
	return s.client.ListModels(ctx, opts...)
}

func (c *Client) request(ctx context.Context, method, path string, body []byte, dst any, opts []CallOption) (*HTTPResponse, error) {
	if ctx == nil {
		return nil, invalid("context", "must not be nil")
	}
	if c.closed.Load() {
		return nil, invalid("client", "client is closed")
	}
	o, err := c.callOptions(opts)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptStart := time.Now()
		raw, err := c.attempt(ctx, method, path, body, dst, o, attempt)
		elapsed := time.Since(started)
		if raw != nil {
			raw.Attempts = attempt + 1
			raw.Duration = elapsed
		}
		event := Event{Method: method, Path: path, Attempt: attempt + 1, Duration: time.Since(attemptStart)}
		if raw != nil {
			event.StatusCode = raw.StatusCode
			event.RequestID = raw.RequestID
		}
		if err == nil {
			c.observe(event)
			return raw, nil
		}
		event.ErrorKind = errorCategory(err)
		// Context cancellation must not be converted into a model answer or
		// overridden by a user retry predicate.
		if ctx.Err() != nil {
			c.observe(event)
			return nil, ctx.Err()
		}
		if attempt >= o.retry.MaxRetries || !o.retry.retryable(err) {
			c.observe(event)
			return nil, err
		}
		delay := o.retry.delay(attempt, err)
		if o.retry.TotalTimeout > 0 && (elapsed >= o.retry.TotalTimeout || delay >= o.retry.TotalTimeout-elapsed) {
			c.observe(event)
			return nil, err
		}
		if deadline, ok := ctx.Deadline(); ok && delay >= time.Until(deadline) {
			c.observe(event)
			return nil, err
		}
		event.WillRetry = true
		event.RetryDelay = delay
		c.observe(event)
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
}
func (c *Client) observe(e Event) {
	if c.observer != nil {
		c.observer(e)
	}
}

func (c *Client) attempt(parent context.Context, method, path string, body []byte, dst any, o callConfig, attempt int) (*HTTPResponse, error) {
	ctx, cancel := context.WithTimeout(parent, o.timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, invalid("request", "could not construct HTTP request")
	}
	req.Header = o.headers.Clone()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if attempt > 0 {
		req.Header.Set("X-TypeSafe-Retry-Count", strconv.Itoa(attempt))
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, transportError(err, parent, ctx, o.timeout)
	}
	defer resp.Body.Close()
	raw := &HTTPResponse{StatusCode: resp.StatusCode, Header: resp.Header.Clone(), RequestID: resp.Header.Get(RequestIDHeader)}
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return raw, transportError(err, parent, ctx, o.timeout)
	}
	if int64(len(data)) > c.maxResponseBytes {
		return raw, &ResponseTooLargeError{Limit: c.maxResponseBytes}
	}
	raw.Body = data
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, &APIError{Kind: kindForStatus(resp.StatusCode), StatusCode: resp.StatusCode,
			Body: data, Header: raw.Header, RequestID: raw.RequestID, Endpoint: method + " " + c.baseURL + path,
			Message: extractMessage(data), RetryAfter: parseRetryAfter(resp.Header, time.Now())}
	}
	if err := json.Unmarshal(data, dst); err != nil {
		var validation *ResponseValidationError
		if errors.As(err, &validation) {
			validation.HTTP = raw
			return raw, validation
		}
		return raw, &ResponseValidationError{Field: "", Message: "body does not match destination schema", HTTP: raw}
	}
	if validator, ok := dst.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return raw, &ResponseValidationError{Field: "", Message: "custom response validation failed", HTTP: raw}
		}
	}
	return raw, nil
}

func transportError(err error, parent, attempt context.Context, timeout time.Duration) error {
	if parent.Err() != nil {
		return parent.Err()
	}
	var ne net.Error
	if attempt.Err() == context.DeadlineExceeded || (errors.As(err, &ne) && ne.Timeout()) || errors.Is(err, context.DeadlineExceeded) {
		return &TimeoutError{Cause: err, Timeout: timeout}
	}
	return &ConnectionError{Cause: err}
}
func errorCategory(err error) string {
	var api *APIError
	var timeout *TimeoutError
	var conn *ConnectionError
	var validation *ResponseValidationError
	switch {
	case errors.As(err, &api):
		return string(api.Kind)
	case errors.As(err, &timeout):
		return "timeout"
	case errors.As(err, &conn):
		return "connection"
	case errors.As(err, &validation):
		return "response_validation"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	default:
		return "client_error"
	}
}
func extractMessage(data []byte) string {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return string(data)
	}
	if s, ok := value.(string); ok {
		return s
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"error", "message", "detail"} {
		if s, ok := obj[key].(string); ok {
			return s
		}
		if m, ok := obj[key].(map[string]any); ok {
			if s, ok := m["message"].(string); ok {
				return s
			}
		}
	}
	if details, ok := obj["detail"].([]any); ok {
		parts := []string{}
		for _, d := range details {
			m, ok := d.(map[string]any)
			if !ok {
				continue
			}
			message, ok := m["msg"].(string)
			if !ok {
				continue
			}
			path := []string{}
			if location, ok := m["loc"].([]any); ok {
				for _, part := range location {
					if part != "body" {
						path = append(path, fmt.Sprint(part))
					}
				}
			}
			if len(path) > 0 {
				message = strings.Join(path, ".") + ": " + message
			}
			parts = append(parts, message)
		}
		return strings.Join(parts, "; ")
	}
	return ""
}
