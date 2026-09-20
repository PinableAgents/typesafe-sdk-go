package typesafe

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func mixedQuestions() Questions {
	return Questions{
		"intent": ChoiceLabels("intent?", "explain", "change"),
		"write":  Noul{},
		"scope":  Score{Criteria: []any{"Local", map[string]any{"description": "Broad"}}},
	}
}

func TestMixedResponseAndIntegerKeys(t *testing.T) {
	data, err := os.ReadFile("testdata/mixed_response.json")
	if err != nil {
		t.Fatal(err)
	}
	var r SystemOneResponse
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	if err := r.ValidateFor(mixedQuestions()); err != nil {
		t.Fatal(err)
	}
	if len(r.Answers) != 3 || r.Choices["intent"].Choice != "explain" || r.Scores["scope"].Probabilities[1] != 0.3 {
		t.Fatal("typed views")
	}
	if _, ok := r.Scores["scope"].Legend[1].(map[string]any); !ok {
		t.Fatal("structured legend was lost")
	}
	if r.Usage.InputTokens == nil || *r.Usage.InputTokens != 120 {
		t.Fatal("usage")
	}
	marshaled, err := json.Marshal(r)
	if err != nil || !strings.Contains(string(marshaled), `"1":0.3`) {
		t.Fatal("wire keys must remain strings", err)
	}
}

func TestUnknownAnswerForwardCompatibility(t *testing.T) {
	var r SystemOneResponse
	if err := json.Unmarshal([]byte(`{"model":"mock","usage":{"input_tokens":null},"answers":{"q":{"type":"future","result":"x"}}}`), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Answers) != 0 || len(r.UnknownAnswers) != 1 {
		t.Fatal("unknown type handling")
	}
	if err := r.ValidateFor(minimalRequest().Questions); err == nil {
		t.Fatal("agent must reject a missing known answer")
	}
	if err := json.Unmarshal([]byte(`{"model":"mock","usage":{}}`), &r); err != nil || len(r.Answers) != 0 {
		t.Fatal("upstream default answers should be empty", err)
	}
}

func TestResponseRejectsMalformedFields(t *testing.T) {
	cases := map[string]string{
		"non_object":          `[]`,
		"missing_model":       `{"usage":{}}`,
		"null_model":          `{"model":null,"usage":{}}`,
		"model_wrong_type":    `{"model":3,"usage":{}}`,
		"missing_usage":       `{"model":"m"}`,
		"null_usage":          `{"model":"m","usage":null}`,
		"bad_tokens":          `{"model":"m","usage":{"input_tokens":"12"}}`,
		"negative_tokens":     `{"model":"m","usage":{"input_tokens":-1}}`,
		"fractional_tokens":   `{"model":"m","usage":{"input_tokens":1.5}}`,
		"answers_null":        `{"model":"m","usage":{},"answers":null}`,
		"answer_not_object":   `{"model":"m","usage":{},"answers":{"q":false}}`,
		"missing_type":        `{"model":"m","usage":{},"answers":{"q":{}}}`,
		"null_type":           `{"model":"m","usage":{},"answers":{"q":{"type":null}}}`,
		"null_noul":           `{"model":"m","usage":{},"answers":{"q":{"type":"noul","noul":null}}}`,
		"string_noul":         `{"model":"m","usage":{},"answers":{"q":{"type":"noul","noul":"0.5"}}}`,
		"noul_range":          `{"model":"m","usage":{},"answers":{"q":{"type":"noul","noul":1.1}}}`,
		"no_confidence":       `{"model":"m","usage":{},"answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":1}}}}`,
		"bad_confidence":      `{"model":"m","usage":{},"answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":1},"confidence":2}}}`,
		"null_choice":         `{"model":"m","usage":{},"answers":{"q":{"type":"choice","choice":null,"probabilities":{"a":1},"confidence":0.8}}}`,
		"null_probability":    `{"model":"m","usage":{},"answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":null},"confidence":0.8}}}`,
		"bad_probability":     `{"model":"m","usage":{},"answers":{"q":{"type":"choice","choice":"a","probabilities":{"a":-0.1},"confidence":0.8}}}`,
		"probability_not_map": `{"model":"m","usage":{},"answers":{"q":{"type":"choice","choice":"a","probabilities":[],"confidence":0.8}}}`,
		"score_missing":       `{"model":"m","usage":{},"answers":{"q":{"type":"score","probabilities":{"0":1},"legend":{"0":"one"},"confidence":0.8}}}`,
		"legend_missing":      `{"model":"m","usage":{},"answers":{"q":{"type":"score","score":0,"probabilities":{"0":1},"confidence":0.8}}}`,
		"legend_null_value":   `{"model":"m","usage":{},"answers":{"q":{"type":"score","score":0,"probabilities":{"0":1},"legend":{"0":null},"confidence":0.8}}}`,
		"legend_bad_key":      `{"model":"m","usage":{},"answers":{"q":{"type":"score","score":0,"probabilities":{"0":1},"legend":{"x":"x"},"confidence":0.8}}}`,
		"score_bad_key":       `{"model":"m","usage":{},"answers":{"q":{"type":"score","score":0,"probabilities":{"01":1},"legend":{"0":"x"},"confidence":0.8}}}`,
		"trailing_json":       minimalResponse + `{}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			var r SystemOneResponse
			if err := json.Unmarshal([]byte(data), &r); err == nil {
				t.Fatal("malformed response accepted")
			}
		})
	}
}

func TestValidateForSemanticMismatch(t *testing.T) {
	cases := map[string]func(*SystemOneResponse){
		"missing":             func(r *SystemOneResponse) { delete(r.Answers, "intent") },
		"type_mismatch":       func(r *SystemOneResponse) { r.Answers["intent"] = NoulAnswer{} },
		"choice_missing_view": func(r *SystemOneResponse) { delete(r.Choices, "intent") },
		"choice_outside_set":  func(r *SystemOneResponse) { v := r.Choices["intent"]; v.Choice = "invented"; r.Choices["intent"] = v },
		"not_maximum":         func(r *SystemOneResponse) { v := r.Choices["intent"]; v.Choice = "change"; r.Choices["intent"] = v },
		"sum_wrong":           func(r *SystemOneResponse) { r.Choices["intent"].Probabilities["explain"] = 0.8 },
		"option_missing":      func(r *SystemOneResponse) { delete(r.Choices["intent"].Probabilities, "change") },
		"option_replaced": func(r *SystemOneResponse) {
			delete(r.Choices["intent"].Probabilities, "change")
			r.Choices["intent"].Probabilities["other"] = 0.05
		},
		"noul_missing_view":   func(r *SystemOneResponse) { delete(r.Nouls, "write") },
		"score_missing_view":  func(r *SystemOneResponse) { delete(r.Scores, "scope") },
		"score_wrong_weight":  func(r *SystemOneResponse) { v := r.Scores["scope"]; v.Score = 1.5; r.Scores["scope"] = v },
		"score_missing_level": func(r *SystemOneResponse) { delete(r.Scores["scope"].Probabilities, 1) },
		"score_level_replaced": func(r *SystemOneResponse) {
			delete(r.Scores["scope"].Probabilities, 1)
			r.Scores["scope"].Probabilities[2] = 0.3
		},
		"score_legend_replaced": func(r *SystemOneResponse) { delete(r.Scores["scope"].Legend, 1); r.Scores["scope"].Legend[2] = "wrong" },
		"score_bad_sum":         func(r *SystemOneResponse) { r.Scores["scope"].Probabilities[1] = 0.1 },
	}
	data, err := os.ReadFile("testdata/mixed_response.json")
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			var r SystemOneResponse
			_ = json.Unmarshal(data, &r)
			change(&r)
			var ve *ResponseValidationError
			if err := r.ValidateFor(mixedQuestions()); !errors.As(err, &ve) {
				t.Fatal("semantic mismatch accepted", err)
			}
		})
	}
	var nilResponse *SystemOneResponse
	if nilResponse.ValidateFor(mixedQuestions()) == nil {
		t.Fatal("nil response accepted")
	}
}

func TestScoreAcceptsRoundingDeviation(t *testing.T) {
	// Mirrors a real service answer: three levels, probabilities rounded to two
	// decimals, and a continuous score that is one rounding step off the
	// weighted value. Tolerance for three levels is 0.005 * (0 + 1 + 2) = 0.015.
	const body = `{"model":"jev","answers":{"scope":{"type":"score","score":0.46,` +
		`"confidence":0.5,"legend":{"0":"One local issue.","1":"One component.","2":"Several components."},` +
		`"probabilities":{"0":0.54,"1":0.46,"2":0.0}}},"usage":{}}`
	questions := Questions{"scope": ScoreLevels("How broad?", "One local issue.", "One component.", "Several components.")}
	for _, gap := range []float64{0.005, 0.01} {
		var r SystemOneResponse
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		v := r.Scores["scope"]
		v.Score += gap
		r.Scores["scope"] = v
		if err := r.ValidateFor(questions); err != nil {
			t.Errorf("gap %.3f rejected: %v", gap, err)
		}
	}
	for _, gap := range []float64{0.02, 0.5} {
		var r SystemOneResponse
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Fatal(err)
		}
		v := r.Scores["scope"]
		v.Score += gap
		r.Scores["scope"] = v
		if err := r.ValidateFor(questions); err == nil {
			t.Errorf("gap %.3f accepted", gap)
		}
	}
}

func TestModelsValidation(t *testing.T) {
	for _, data := range []string{`[]`, `{}`, `{"models":null}`, `{"models":[{}]}`, `{"models":[{"name":"x"}]}`, `{"models":[{"name":"x","description":"x","release_date":null}]}`} {
		var r ListModelsResponse
		if err := json.Unmarshal([]byte(data), &r); err == nil {
			t.Error("bad model list accepted", data)
		}
	}
}
