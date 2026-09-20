package typesafe

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

const (
	Version                       = "0.2.0"
	DefaultBaseURL                = "https://api.typesafe.ai"
	DefaultModel                  = "jev-latest"
	DefaultTimeout                = 10 * time.Second
	APIKeyEnv                     = "TYPESAFE_API_KEY"
	BaseURLEnv                    = "TYPESAFE_BASE_URL"
	DefaultModelEnv               = "TYPESAFE_DEFAULT_MODEL"
	RequestIDHeader               = "X-TypeSafe-Request-Id"
	defaultMaxResponseBytes int64 = 8 << 20
)

// JSONContent accepts JSON text, objects, and arrays. Nested values can be any
// JSON value. Use json.RawMessage, not []byte, for pre-encoded JSON.
type JSONContent = any

type Question interface {
	json.Marshaler
	isQuestion()
}

type Questions map[string]Question

// Choice selects a label. A nil criterion means an undescribed label.
type Choice struct {
	Instructions JSONContent
	Criteria     map[string]JSONContent
}

// Score uses a nonempty ordered rubric, starting at level zero.
// Two or more meaningful levels are recommended by the HTTP API documentation.
type Score struct {
	Instructions JSONContent
	Criteria     []JSONContent
}

// Noul asks for the probability of a statement being true.
type Noul struct {
	Instructions JSONContent
	Criteria     NoulCriteria
}

// NoulCriteria permits only "true" and "false", with optional null values.
type NoulCriteria map[string]JSONContent

// RawQuestion is an escape hatch for dictionary-style questions and future types.
// It must contain a nonempty string "type". Known kinds are still shape-checked.
type RawQuestion map[string]any

func (Choice) isQuestion()      {}
func (Score) isQuestion()       {}
func (Noul) isQuestion()        {}
func (RawQuestion) isQuestion() {}

func (q Choice) MarshalJSON() ([]byte, error) {
	return marshalQuestion("choice", q.Instructions, q.Criteria, true)
}
func (q Score) MarshalJSON() ([]byte, error) {
	return marshalQuestion("score", q.Instructions, q.Criteria, true)
}
func (q Noul) MarshalJSON() ([]byte, error) {
	return marshalQuestion("noul", q.Instructions, q.Criteria, q.Criteria != nil)
}
func (q RawQuestion) MarshalJSON() ([]byte, error) { return json.Marshal(map[string]any(q)) }

func marshalQuestion(kind string, instructions any, criteria any, hasCriteria bool) ([]byte, error) {
	b := map[string]any{"type": kind}
	if instructions != nil {
		raw, err := json.Marshal(instructions)
		if err != nil {
			return nil, err
		}
		if string(raw) != "null" {
			b["instructions"] = json.RawMessage(raw)
		}
	}
	if hasCriteria {
		b["criteria"] = criteria
	}
	return json.Marshal(b)
}

func ChoiceLabels(instructions JSONContent, labels ...string) Choice {
	criteria := make(map[string]JSONContent, len(labels))
	for _, label := range labels {
		criteria[label] = nil
	}
	return Choice{Instructions: instructions, Criteria: criteria}
}

func ScoreLevels(instructions JSONContent, levels ...string) Score {
	criteria := make([]JSONContent, len(levels))
	for i, level := range levels {
		criteria[i] = level
	}
	return Score{Instructions: instructions, Criteria: criteria}
}

type SystemOneRequest struct {
	State     JSONContent
	Questions Questions
	Model     string
	// ExtraBody is shallow-merged last, including collisions with core fields.
	// This advanced feature can change the effective questions. ValidateFor must
	// receive the effective questions, not the overwritten originals.
	ExtraBody map[string]any
}

type Answer interface{ AnswerType() string }

type NoulAnswer struct {
	Type string  `json:"type"`
	Noul float64 `json:"noul"`
}

func (NoulAnswer) AnswerType() string { return "noul" }

type ChoiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

func (ChoiceAnswer) AnswerType() string { return "choice" }

type ScoreAnswer struct {
	Type          string              `json:"type"`
	Score         float64             `json:"score"`
	Legend        map[int]JSONContent `json:"legend"`
	Probabilities map[int]float64     `json:"probabilities"`
	Confidence    float64             `json:"confidence"`
}

func (ScoreAnswer) AnswerType() string { return "score" }

// Pointers distinguish unreported usage from an actual zero count.
type Usage struct {
	InputTokens  *int64 `json:"input_tokens"`
	OutputTokens *int64 `json:"output_tokens"`
}

type HTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	RequestID  string
	Attempts   int
	Duration   time.Duration
}

type SystemOneResponse struct {
	Model   string                  `json:"model"`
	Usage   Usage                   `json:"usage"`
	Answers map[string]Answer       `json:"answers"`
	Nouls   map[string]NoulAnswer   `json:"-"`
	Choices map[string]ChoiceAnswer `json:"-"`
	Scores  map[string]ScoreAnswer  `json:"-"`
	// Future answer kinds do not fail the whole response. Agent policies should
	// call ValidateFor before acting, so missing/changed answer kinds fail closed.
	UnknownAnswers map[string]json.RawMessage `json:"-"`
	HTTP           *HTTPResponse              `json:"-"`
}

type ModelMetadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

type ListModelsResponse struct {
	Models []ModelMetadata `json:"models"`
	HTTP   *HTTPResponse   `json:"-"`
}

// Evaluator lets application policies use either a Client or a test double.
type Evaluator interface {
	SystemOne(context.Context, SystemOneRequest, ...CallOption) (*SystemOneResponse, error)
}
