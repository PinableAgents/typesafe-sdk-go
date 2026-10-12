// Package agenttool exposes provider-neutral JSON tools backed by the Go SDK.
// Register the definitions and Call callback in your host's tool registry.
// This is not an MCP server and never grants permission to execute other tools.
package agenttool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	typesafe "github.com/PinableAgents/typesafe-sdk-go"
	"github.com/PinableAgents/typesafe-sdk-go/contrib/agentpolicy"
)

const (
	ListModels       = "typesafe_list_models"
	Evaluate         = "typesafe_evaluate"
	RouteTask        = "typesafe_route_task"
	MaxArgumentBytes = 64 << 10
	MaxQuestions     = 32
	CallTimeout      = 20 * time.Second
)

// API is implemented by *typesafe.Client. Custom implementations must be safe
// for concurrent calls and honor context cancellation.
type API interface {
	typesafe.Evaluator
	ListModels(context.Context, ...typesafe.CallOption) (*typesafe.ListModelsResponse, error)
}

type Definition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// Definitions returns independent copies. Map input_schema to the parameters
// field expected by the host/model provider; no provider SDK is required here.
func Definitions() []Definition {
	return []Definition{
		{ListModels, "List available TypeSafe models. Sends no workspace content.", json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
		{Evaluate, "Evaluate supplied state using Choice, Score or Noul questions. Only send necessary redacted data. Answers are advisory, not tool-execution permissions.", json.RawMessage(`{"type":"object","required":["state","questions"],"properties":{"state":{"type":["string","object","array"]},"model":{"type":"string"},"questions":{"type":"object","minProperties":1,"maxProperties":32,"additionalProperties":{"oneOf":[{"type":"object","required":["type","criteria"],"properties":{"type":{"const":"choice"},"instructions":{"type":["string","object","array","null"]},"criteria":{"type":"object","minProperties":1,"additionalProperties":{"type":["string","object","array","null"]}}},"additionalProperties":false},{"type":"object","required":["type","criteria"],"properties":{"type":{"const":"score"},"instructions":{"type":["string","object","array","null"]},"criteria":{"type":"array","minItems":1,"items":{"type":["string","object","array"]}}},"additionalProperties":false},{"type":"object","required":["type"],"properties":{"type":{"const":"noul"},"instructions":{"type":["string","object","array","null"]},"criteria":{"type":["object","null"],"properties":{"true":{"type":["string","object","array","null"]},"false":{"type":["string","object","array","null"]}},"additionalProperties":false}},"additionalProperties":false,"anyOf":[{"required":["instructions"],"properties":{"instructions":{"type":["string","object","array"]}}},{"required":["criteria"],"properties":{"criteria":{"type":"object","anyOf":[{"required":["true"],"properties":{"true":{"type":["string","object","array"]}}},{"required":["false"],"properties":{"false":{"type":["string","object","array"]}}}]}}}]}]}}},"additionalProperties":false}`)},
		{RouteTask, "Suggest an Agent task route. Review is required for failures, uncertainty or state changes. Never use the result to bypass host permissions or approvals.", json.RawMessage(`{"type":"object","required":["task"],"properties":{"task":{"type":"string","minLength":1},"context_summary":{"type":"string"}},"additionalProperties":false}`)},
	}
}

type ToolError struct {
	Code           string `json:"code"`
	Message        string `json:"message"`
	HTTPStatus     int    `json:"http_status,omitempty"`
	Retryable      bool   `json:"retryable"`
	RequiresReview bool   `json:"requires_review"`
}

// Metadata contains only selected diagnostic values, never HTTP bodies, headers,
// endpoint URLs or credentials. Missing values are unreported, not measured zero.
type Metadata struct {
	RequestID      string `json:"request_id,omitempty"`
	DurationMillis int64  `json:"duration_ms,omitempty"`
	Attempts       int    `json:"attempts,omitempty"`
	Model          string `json:"model,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	RubricVersion  string `json:"rubric_version,omitempty"`
}

func responseMetadata(model string, raw *typesafe.HTTPResponse) *Metadata {
	m := &Metadata{Model: model}
	if raw != nil {
		m.RequestID, m.DurationMillis, m.Attempts, m.Protocol = raw.RequestID, raw.Duration.Milliseconds(), raw.Attempts, raw.Protocol
	}
	return m
}

type Result struct {
	Metadata *Metadata  `json:"metadata,omitempty"`
	OK       bool       `json:"ok"`
	Data     any        `json:"data,omitempty"`
	Error    *ToolError `json:"error,omitempty"`
}

// ErrorResult exposes only fixed messages and error categories. It never
// serializes err.Error(), API response bodies, headers, or connection URLs.
// Transport failures are NOT automatically marked retryable: the service may
// already have processed an evaluation when a connection breaks.
func ErrorResult(err error) Result {
	e := &ToolError{Code: "evaluation_failed", Message: "Evaluation failed; no authorization decision was made.", RequiresReview: true}
	var api *typesafe.APIError
	var invalid *typesafe.ValidationError
	var response *typesafe.ResponseValidationError
	var connection *typesafe.ConnectionError
	var timeout *typesafe.TimeoutError
	var size *typesafe.ResponseTooLargeError
	switch {
	case errors.Is(err, context.Canceled):
		e.Code, e.Message = "canceled", "The caller canceled the request."
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout):
		e.Code, e.Message = "timeout", "The evaluation deadline was exceeded."
	case errors.As(err, &api):
		e.Code, e.Message, e.HTTPStatus = string(api.Kind), "TypeSafe returned an HTTP error.", api.StatusCode
		e.Retryable = api.StatusCode == 429 || api.StatusCode == 529
	case errors.As(err, &invalid):
		e.Code, e.Message = "invalid_arguments", "The arguments do not match the tool contract."
	case errors.As(err, &response):
		e.Code, e.Message = "invalid_response", "The response did not satisfy the requested answer contract."
	case errors.As(err, &connection):
		e.Code, e.Message = "connection_failed", "No usable HTTP response; check DNS, proxy, TLS and connectivity."
	case errors.As(err, &size):
		e.Code, e.Message = "response_too_large", "The configured response size limit was exceeded."
	}
	result := Result{Error: e}
	if api != nil {
		result.Metadata = &Metadata{RequestID: api.RequestID}
	}
	if response != nil {
		result.Metadata = responseMetadata("", response.HTTP)
	}
	return result
}

func invalidArguments() Result {
	return ErrorResult(&typesafe.ValidationError{})
}

// Registry borrows the API client; the host owns its lifetime. A single Registry
// can be shared by concurrent calls if its API implementation supports that.
type Registry struct {
	api    API
	router *agentpolicy.Router
}

func New(api API) (*Registry, error) {
	cfg := agentpolicy.DefaultConfig()
	cfg.Timeout = CallTimeout
	return NewWithConfig(api, cfg)
}

// NewWithConfig allows trusted hosts to configure routing thresholds. This is
// a host-side option; JSON tool arguments still cannot override policy or URLs.
func NewWithConfig(api API, cfg agentpolicy.Config) (*Registry, error) {
	if api == nil {
		return nil, errors.New("agenttool: nil API")
	}
	v := reflect.ValueOf(api)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return nil, errors.New("agenttool: nil API")
		}
	}
	router, err := agentpolicy.NewRouter(api, cfg)
	if err != nil {
		return nil, err
	}
	return &Registry{api: api, router: router}, nil
}

// Invoke accepts one JSON envelope: {"tool":"...","arguments":{...}}.
// No API keys, URLs, headers, retry policies or ExtraBody overrides are accepted.
func (r *Registry) Invoke(ctx context.Context, input json.RawMessage) Result {
	var call struct {
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if strictObject(input, &call) != nil {
		return invalidArguments()
	}
	return r.Call(ctx, call.Tool, call.Arguments)
}

// Call validates tool input and the returned answer contract before producing
// data. Evaluation uses no implicit retries. RouteTask keeps agentpolicy's
// bounded HTTP-status retry, but does not retry ambiguous transport failures.
func (r *Registry) Call(ctx context.Context, name string, input json.RawMessage) Result {
	if r == nil || r.api == nil || ctx == nil {
		return invalidArguments()
	}
	if err := ctx.Err(); err != nil {
		return ErrorResult(err)
	}
	ctx, cancel := context.WithTimeout(ctx, CallTimeout)
	defer cancel()
	switch name {
	case ListModels:
		var args struct{}
		if strictObject(input, &args) != nil {
			return invalidArguments()
		}
		result, err := r.api.ListModels(ctx, typesafe.WithRetry(typesafe.RetryPolicy{}))
		if err != nil {
			return ErrorResult(err)
		}
		if result == nil || len(result.Models) == 0 {
			return ErrorResult(&typesafe.ResponseValidationError{})
		}
		return Result{OK: true, Data: result, Metadata: responseMetadata("", result.HTTP)}
	case Evaluate:
		var args struct {
			State     json.RawMessage            `json:"state"`
			Questions map[string]json.RawMessage `json:"questions"`
			Model     string                     `json:"model,omitempty"`
		}
		if strictObject(input, &args) != nil || len(args.Questions) == 0 || len(args.Questions) > MaxQuestions {
			return invalidArguments()
		}
		state := bytes.TrimSpace(args.State)
		if len(state) == 0 || (state[0] != '"' && state[0] != '{' && state[0] != '[') {
			return invalidArguments()
		}
		questions := make(typesafe.Questions, len(args.Questions))
		for id, raw := range args.Questions {
			var q struct {
				Type         string          `json:"type"`
				Instructions json.RawMessage `json:"instructions"`
				Criteria     json.RawMessage `json:"criteria"`
			}
			if strings.TrimSpace(id) == "" || strictObject(raw, &q) != nil {
				return invalidArguments()
			}
			if q.Type != "choice" && q.Type != "score" && q.Type != "noul" {
				return invalidArguments()
			}
			object := typesafe.RawQuestion{"type": q.Type}
			if len(q.Instructions) != 0 {
				object["instructions"] = q.Instructions
			}
			if len(q.Criteria) != 0 {
				object["criteria"] = q.Criteria
			}
			questions[id] = object
		}
		if err := typesafe.ValidateQuestions(questions); err != nil {
			return invalidArguments()
		}
		result, err := r.api.SystemOne(ctx, typesafe.SystemOneRequest{State: args.State, Questions: questions, Model: args.Model}, typesafe.WithRetry(typesafe.RetryPolicy{}))
		if err != nil {
			return ErrorResult(err)
		}
		if err := result.ValidateFor(questions); err != nil {
			out := ErrorResult(err)
			if result != nil {
				out.Metadata = responseMetadata(result.Model, result.HTTP)
			}
			return out
		}
		return Result{OK: true, Data: result, Metadata: responseMetadata(result.Model, result.HTTP)}
	case RouteTask:
		var task agentpolicy.Task
		if strictObject(input, &task) != nil || strings.TrimSpace(task.Text) == "" {
			return invalidArguments()
		}
		decision, err := r.router.Evaluate(ctx, task)
		if err != nil {
			result := ErrorResult(err)
			result.Data = decision // Keeps the fail-closed decision visible to the host.
			return result
		}
		return Result{OK: true, Data: decision, Metadata: &Metadata{
			RequestID: decision.RequestID, DurationMillis: decision.DurationMillis,
			Attempts: decision.Attempts, Model: decision.Model, RubricVersion: decision.RubricVersion,
		}}
	default:
		return Result{Error: &ToolError{Code: "unknown_tool", Message: "Unknown TypeSafe tool name.", RequiresReview: true}}
	}
}

// Reject unknown properties, trailing values and invalid UTF-8. The SDK does
// the question-specific validation before HTTP; schema metadata is not relied
// on as a substitute for runtime checks.
func strictObject(raw []byte, dst any) error {
	if len(raw) > MaxArgumentBytes {
		return errors.New("object too large")
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' || !utf8.Valid(raw) {
		return errors.New("invalid object")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
