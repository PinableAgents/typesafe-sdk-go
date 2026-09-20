package typesafe

import (
	"errors"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy is copied at construction/call boundaries. Start with
// DefaultRetryPolicy and change fields: a zero value means NO retries.
type RetryPolicy struct {
	MaxRetries         int
	BackoffInitial     time.Duration
	BackoffMax         time.Duration
	BackoffJitter      float64
	HTTPStatuses       []int
	RespectRetryAfter  bool
	APIConnectionError bool
	APITimeoutError    bool
	// TotalTimeout is a retry-scheduling budget, not a hard call deadline.
	// Zero disables it. Use context.WithTimeout for a hard outer deadline.
	TotalTimeout time.Duration
	// Predicate supplements built-in rules; caller cancellation always wins.
	Predicate func(error) bool
}

func DefaultRetryPolicy() RetryPolicy {
	statuses := []int{408, 429}
	for i := 500; i < 600; i++ {
		statuses = append(statuses, i)
	}
	return RetryPolicy{MaxRetries: 2, BackoffInitial: 500 * time.Millisecond, BackoffMax: 5 * time.Second,
		BackoffJitter: 0.25, HTTPStatuses: statuses, RespectRetryAfter: true,
		APIConnectionError: true, APITimeoutError: true, TotalTimeout: 30 * time.Second}
}
func cloneRetry(p RetryPolicy) RetryPolicy {
	p.HTTPStatuses = append([]int(nil), p.HTTPStatuses...)
	return p
}
func (p RetryPolicy) validate() error {
	if p.MaxRetries < 0 {
		return invalid("retry.max_retries", "must not be negative")
	}
	if p.BackoffInitial < 0 || p.BackoffMax < 0 || p.TotalTimeout < 0 {
		return invalid("retry", "durations must not be negative")
	}
	if !probabilityOK(p.BackoffJitter) {
		return invalid("retry.backoff_jitter", "must be within [0,1]")
	}
	for _, status := range p.HTTPStatuses {
		if status < 100 || status > 599 {
			return invalid("retry.http_statuses", "invalid HTTP status")
		}
	}
	return nil
}
func (p RetryPolicy) retryable(err error) bool {
	var timeout *TimeoutError
	var connection *ConnectionError
	var api *APIError
	builtin := false
	switch {
	case errors.As(err, &timeout):
		builtin = p.APITimeoutError
	case errors.As(err, &connection):
		builtin = p.APIConnectionError
	case errors.As(err, &api):
		for _, s := range p.HTTPStatuses {
			if s == api.StatusCode {
				builtin = true
				break
			}
		}
	}
	return builtin || (p.Predicate != nil && p.Predicate(err))
}
func (p RetryPolicy) delay(attempt int, err error) time.Duration {
	var api *APIError
	if p.RespectRetryAfter && errors.As(err, &api) && api.RetryAfter != nil {
		return *api.RetryAfter
	}
	if p.BackoffInitial == 0 || p.BackoffMax == 0 {
		return 0
	}
	d := p.BackoffInitial
	if d > p.BackoffMax {
		d = p.BackoffMax
	}
	for i := 0; i < attempt && d < p.BackoffMax; i++ {
		if d > p.BackoffMax/2 {
			d = p.BackoffMax
		} else {
			d *= 2
		}
	}
	// The package-level generator is concurrency-safe; this is not cryptography.
	return time.Duration(float64(d) * (1 - rand.Float64()*p.BackoffJitter))
}

func parseRetryAfter(h http.Header, now time.Time) *time.Duration {
	for _, candidate := range []struct {
		name       string
		multiplier float64
	}{
		{"Retry-After-Ms", float64(time.Millisecond)}, {"Retry-After", float64(time.Second)},
	} {
		values, exists := h[http.CanonicalHeaderKey(candidate.name)]
		if !exists || len(values) == 0 {
			continue
		}
		raw := strings.TrimSpace(values[0])
		if raw == "" {
			raw = "0"
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) && f >= 0 {
			ns := f * candidate.multiplier
			if math.IsInf(ns, 0) || ns >= float64(math.MaxInt64) {
				d := time.Duration(math.MaxInt64)
				return &d
			}
			d := time.Duration(ns)
			return &d
		}
		if candidate.name == "Retry-After" {
			if date, err := http.ParseTime(raw); err == nil {
				d := date.Sub(now)
				if d < 0 {
					d = 0
				}
				return &d
			}
		}
	}
	return nil
}
