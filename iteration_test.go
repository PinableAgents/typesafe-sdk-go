package typesafe

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
)

func TestPython074InputValidation(t *testing.T) {
	cases := []struct {
		name  string
		q     Question
		valid bool
	}{
		{"typed_empty_choice", Choice{Criteria: map[string]any{}}, false},
		{"raw_empty_choice", RawQuestion{"type": "choice", "criteria": map[string]any{}}, false},
		{"typed_empty_noul", Noul{}, false},
		{"raw_empty_noul", RawQuestion{"type": "noul"}, false},
		{"typed_null_outcomes", Noul{Criteria: NoulCriteria{"true": nil, "false": nil}}, false},
		{"raw_null_outcomes", RawQuestion{"type": "noul", "criteria": map[string]any{"true": nil, "false": nil}}, false},
		{"raw_null_instructions", RawQuestion{"type": "noul", "instructions": nil, "criteria": nil}, false},
		{"empty_criteria_no_instructions", Noul{Criteria: NoulCriteria{}}, false},
		{"empty_string_instruction", Noul{Instructions: ""}, true},
		{"empty_object_instruction", Noul{Instructions: map[string]any{}}, true},
		{"empty_array_instruction", Noul{Instructions: []any{}}, true},
		{"empty_criteria_with_instruction", Noul{Instructions: "Question?", Criteria: NoulCriteria{}}, true},
		{"only_true_description", Noul{Criteria: NoulCriteria{"true": ""}}, true},
		{"only_false_description", Noul{Criteria: NoulCriteria{"false": map[string]any{}}}, true},
		{"valid_choice", ChoiceLabels(nil, "one"), true},
		{"valid_score", ScoreLevels(nil, "one"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateQuestions(Questions{"q": tc.q}); (err == nil) != tc.valid {
				t.Fatalf("valid=%t, got %v", tc.valid, err)
			}
			if !tc.valid {
				count := 0
				c, err := NewClient(Config{APIKey: "fixture", Retry: &RetryPolicy{}, Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { count++; return nil, fmt.Errorf("unexpected request") })})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				_, err = c.SystemOne(context.Background(), SystemOneRequest{State: "test", Questions: Questions{"q": tc.q}})
				if err == nil || count != 0 {
					t.Fatal("invalid input reached HTTP")
				}
			}
		})
	}
}

func TestNonFiniteFloat64ExplicitCompatibility(t *testing.T) {
	for _, tc := range []struct {
		v    float64
		want string
	}{{math.NaN(), `"NaN"`}, {math.Inf(1), `"Infinity"`}, {math.Inf(-1), `"-Infinity"`}, {1.25, `1.25`}, {math.Copysign(0, -1), `-0`}} {
		got, err := json.Marshal(NonFiniteFloat64(tc.v))
		if err != nil || string(got) != tc.want {
			t.Fatalf("%s %v", got, err)
		}
	}
	type record struct {
		Number json.Number      `json:"number"`
		Value  NonFiniteFloat64 `json:"value"`
		Omit   string           `json:"omit,omitempty"`
		Raw    json.RawMessage  `json:"raw"`
	}
	state := record{Number: json.Number("9007199254740993"), Value: NonFiniteFloat64(math.NaN()), Raw: json.RawMessage(`{"n":9007199254740995}`)}
	req := minimalRequest()
	req.State = map[string]any{"nested": []any{state, NonFiniteFloat64(math.Inf(1))}}
	req.Questions["q"] = Noul{Instructions: map[string]any{"n": NonFiniteFloat64(math.Inf(-1))}}
	req.ExtraBody = map[string]any{"metrics": []NonFiniteFloat64{NonFiniteFloat64(math.NaN())}}
	raw, err := prepareBody(req, DefaultModel)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`9007199254740993`, `9007199254740995`, `"NaN"`, `"Infinity"`, `"-Infinity"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("lost %s in %s", want, raw)
		}
	}
	if strings.Contains(string(raw), "omit") {
		t.Fatal("struct tags changed")
	}
	// Ordinary floats still reject non-finite inputs; no implicit rewriting.
	for _, v := range []any{math.NaN(), math.Inf(1), math.Inf(-1), json.RawMessage(`{"bad":NaN}`), json.Number("NaN")} {
		req := minimalRequest()
		req.State = map[string]any{"v": v}
		if _, err := prepareBody(req, DefaultModel); err == nil {
			t.Fatal("strict encoding was relaxed")
		}
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	req.State = cycle
	if _, err := prepareBody(req, DefaultModel); err == nil {
		t.Fatal("cycle accepted")
	}
	req.State = map[string]any{"v": iterationMarshaler{}}
	raw, err = prepareBody(req, DefaultModel)
	if err != nil || !strings.Contains(string(raw), `"custom":true`) {
		t.Fatal("custom marshaler semantics changed", err)
	}
}

type iterationMarshaler struct{}

func (iterationMarshaler) MarshalJSON() ([]byte, error) { return []byte(`{"custom":true}`), nil }

func TestScoreFloatingPointBoundary(t *testing.T) {
	for _, levels := range []int{1, 2, 3, 5, 10} {
		t.Run(fmt.Sprint(levels), func(t *testing.T) {
			criteria := make([]string, levels)
			legend := map[int]any{}
			prob := map[int]float64{}
			for i := range criteria {
				criteria[i] = fmt.Sprint(i)
				legend[i] = criteria[i]
				prob[i] = 0
			}
			weighted := float64(levels-1) / 2
			prob[0] = 0.5
			prob[levels-1] += 0.5
			tolerance := math.Max(1e-3, .0025*float64(levels)*float64(levels-1))
			for _, offset := range []float64{-tolerance, tolerance, -tolerance - 1e-6, tolerance + 1e-6} {
				score := weighted + offset
				want := math.Abs(offset) <= tolerance && score >= 0 && score <= float64(levels-1)
				ans := ScoreAnswer{Type: "score", Score: score, Confidence: 1, Legend: legend, Probabilities: prob}
				r := SystemOneResponse{Answers: map[string]Answer{"q": ans}, Scores: map[string]ScoreAnswer{"q": ans}}
				err := r.ValidateFor(Questions{"q": ScoreLevels("range", criteria...)})
				if (err == nil) != want {
					t.Errorf("score %.17g want=%t err=%v", score, want, err)
				}
			}
		})
	}
	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1e-8, 1 + 1e-8} {
		ans := ScoreAnswer{Type: "score", Score: score, Confidence: 1, Legend: map[int]any{0: "a", 1: "b"}, Probabilities: map[int]float64{0: 1, 1: 0}}
		r := SystemOneResponse{Answers: map[string]Answer{"q": ans}, Scores: map[string]ScoreAnswer{"q": ans}}
		if r.ValidateFor(Questions{"q": ScoreLevels("range", "a", "b")}) == nil {
			t.Fatal("bad score accepted")
		}
	}
}
