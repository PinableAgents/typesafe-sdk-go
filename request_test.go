package typesafe

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestQuestionSerialization(t *testing.T) {
	q := Choice{Criteria: map[string]any{"a": nil, "b": map[string]any{"nested": nil}}}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "instructions") || !strings.Contains(string(data), `"a":null`) || !strings.Contains(string(data), `"nested":null`) {
		t.Fatal(string(data))
	}
	no := Noul{Criteria: NoulCriteria{"true": nil, "false": []any{"not relevant"}}}
	if _, err := normalizeQuestions(Questions{"q": no}); err != nil {
		t.Fatal(err)
	}
	one := ScoreLevels(nil, "Only level")
	if _, err := normalizeQuestions(Questions{"q": one}); err != nil {
		t.Fatal("Python SDK permits one level", err)
	}
	future := RawQuestion{"type": "future_primitive", "extra": 3}
	if _, err := normalizeQuestions(Questions{"q": future}); err != nil {
		t.Fatal("future request escape hatch", err)
	}
	structured := Choice{Instructions: map[string]any{"question": "Which?", "items": []any{true, 2, nil}}, Criteria: map[string]any{"a": []any{"x", nil}}}
	if _, err := normalizeQuestions(Questions{"q": structured}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidQuestions(t *testing.T) {
	cases := []Question{
		nil, (*Choice)(nil), Choice{}, Score{}, Score{Criteria: []any{nil}}, Score{Criteria: []any{3}},
		Noul{Instructions: 123}, Noul{Criteria: NoulCriteria{"yes": "wrong key"}},
		Choice{Criteria: map[string]any{"a": true}}, RawQuestion{}, RawQuestion{"type": 3}, RawQuestion{"type": ""},
		RawQuestion{"type": "score", "criteria": "x"}, RawQuestion{"type": "choice", "criteria": nil}, RawQuestion{"type": "choice"},
		Noul{Instructions: make(chan int)}, Noul{Instructions: math.Inf(1)},
	}
	for i, q := range cases {
		if _, err := normalizeQuestions(Questions{"q": q}); err == nil {
			t.Errorf("invalid question %d accepted", i)
		}
	}
	if _, err := normalizeQuestions(nil); err == nil {
		t.Fatal("empty questions accepted")
	}
}

func TestStateFormatsAndEncodingFailures(t *testing.T) {
	for _, state := range []any{"hello", map[string]any{"x": nil}, []any{1, true, "text"}, json.RawMessage(`{"x":1}`), struct {
		Text string `json:"text"`
	}{"hello"}} {
		req := minimalRequest()
		req.State = state
		if _, err := prepareBody(req, DefaultModel); err != nil {
			t.Error(err)
		}
	}
	for _, state := range []any{nil, 42, false, make(chan int), json.RawMessage(`invalid`)} {
		req := minimalRequest()
		req.State = state
		if _, err := prepareBody(req, DefaultModel); err == nil {
			t.Error("invalid state accepted")
		}
	}
	req := minimalRequest()
	req.ExtraBody = map[string]any{"bad": math.NaN()}
	if _, err := prepareBody(req, DefaultModel); err == nil {
		t.Fatal("invalid extra body accepted")
	}
}
