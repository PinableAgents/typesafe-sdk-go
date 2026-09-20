package typesafe

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

func required[T any](obj map[string]json.RawMessage, key, path string) (T, error) {
	var value T
	raw, ok := obj[key]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return value, invalidResponse(path+key, "missing or null required field")
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, invalidResponse(path+key, "unexpected JSON type")
	}
	return value, nil
}

func parseProbabilityMap(obj map[string]json.RawMessage, path string) (map[string]float64, error) {
	raw, err := required[map[string]json.RawMessage](obj, "probabilities", path)
	if err != nil {
		return nil, err
	}
	p := make(map[string]float64, len(raw))
	for key := range raw {
		value, err := required[float64](raw, key, path+"probabilities.")
		if err != nil {
			return nil, err
		}
		if !probabilityOK(value) {
			return nil, invalidResponse(path+"probabilities."+key, "expected a probability in [0,1]")
		}
		p[key] = value
	}
	return p, nil
}
func probabilityOK(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }

func (r *SystemOneResponse) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return invalidResponse("", "expected a JSON object")
	}
	model, err := required[string](raw, "model", "")
	if err != nil {
		return err
	}
	usageRaw, err := required[map[string]json.RawMessage](raw, "usage", "")
	if err != nil {
		return err
	}
	var usage Usage
	for name, dst := range map[string]**int64{"input_tokens": &usage.InputTokens, "output_tokens": &usage.OutputTokens} {
		if value, exists := usageRaw[name]; exists && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			count, err := required[int64](usageRaw, name, "usage.")
			if err != nil {
				return err
			}
			if count < 0 {
				return invalidResponse("usage."+name, "token count must not be negative")
			}
			*dst = &count
		}
	}
	answers := map[string]json.RawMessage{}
	if _, exists := raw["answers"]; exists {
		answers, err = required[map[string]json.RawMessage](raw, "answers", "")
		if err != nil {
			return err
		}
	}
	result := SystemOneResponse{
		Model: model, Usage: usage,
		Answers: make(map[string]Answer), Nouls: make(map[string]NoulAnswer),
		Choices: make(map[string]ChoiceAnswer), Scores: make(map[string]ScoreAnswer),
		UnknownAnswers: make(map[string]json.RawMessage),
	}
	for name, value := range answers {
		var obj map[string]json.RawMessage
		path := "answers." + name + "."
		if json.Unmarshal(value, &obj) != nil || obj == nil {
			return invalidResponse("answers."+name, "expected an object")
		}
		kind, err := required[string](obj, "type", path)
		if err != nil {
			return err
		}
		if kind != "noul" && kind != "choice" && kind != "score" {
			result.UnknownAnswers[name] = append(json.RawMessage(nil), value...)
			continue
		}
		if kind == "noul" {
			v, err := required[float64](obj, "noul", path)
			if err != nil {
				return err
			}
			if !probabilityOK(v) {
				return invalidResponse(path+"noul", "expected a probability in [0,1]")
			}
			a := NoulAnswer{Type: kind, Noul: v}
			result.Nouls[name], result.Answers[name] = a, a
			continue
		}
		confidence, err := required[float64](obj, "confidence", path)
		if err != nil {
			return err
		}
		if !probabilityOK(confidence) {
			return invalidResponse(path+"confidence", "expected a value in [0,1]")
		}
		probabilities, err := parseProbabilityMap(obj, path)
		if err != nil {
			return err
		}
		if kind == "choice" {
			choice, err := required[string](obj, "choice", path)
			if err != nil {
				return err
			}
			a := ChoiceAnswer{Type: kind, Choice: choice, Probabilities: probabilities, Confidence: confidence}
			result.Choices[name], result.Answers[name] = a, a
			continue
		}
		score, err := required[float64](obj, "score", path)
		if err != nil {
			return err
		}
		legend, err := required[map[string]json.RawMessage](obj, "legend", path)
		if err != nil {
			return err
		}
		a := ScoreAnswer{Type: kind, Score: score, Confidence: confidence, Legend: make(map[int]JSONContent), Probabilities: make(map[int]float64)}
		for key, value := range legend {
			idx, err := scoreIndex(key)
			if err != nil {
				return invalidResponse(path+"legend."+key, "expected a canonical nonnegative integer key")
			}
			if !contentOK(value, false) {
				return invalidResponse(path+"legend."+key, "expected text, object, or array")
			}
			var decoded any
			dec := json.NewDecoder(bytes.NewReader(value))
			dec.UseNumber()
			if dec.Decode(&decoded) != nil {
				return invalidResponse(path+"legend."+key, "invalid JSON content")
			}
			a.Legend[idx] = decoded
		}
		for key, value := range probabilities {
			idx, err := scoreIndex(key)
			if err != nil {
				return invalidResponse(path+"probabilities."+key, "expected a canonical nonnegative integer key")
			}
			a.Probabilities[idx] = value
		}
		result.Scores[name], result.Answers[name] = a, a
	}
	*r = result
	return nil
}

func scoreIndex(key string) (int, error) {
	idx, err := strconv.Atoi(key)
	if err != nil || idx < 0 || strconv.Itoa(idx) != key {
		return 0, invalidResponse("score", "invalid integer key")
	}
	return idx, nil
}

func (r *ListModelsResponse) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil || raw == nil {
		return invalidResponse("", "expected a JSON object")
	}
	entries, err := required[[]map[string]json.RawMessage](raw, "models", "")
	if err != nil {
		return err
	}
	models := make([]ModelMetadata, 0, len(entries))
	for i, obj := range entries {
		path := "models." + strconv.Itoa(i) + "."
		name, err := required[string](obj, "name", path)
		if err != nil {
			return err
		}
		description, err := required[string](obj, "description", path)
		if err != nil {
			return err
		}
		date, err := required[string](obj, "release_date", path)
		if err != nil {
			return err
		}
		models = append(models, ModelMetadata{Name: name, Description: description, ReleaseDate: date})
	}
	r.Models = models
	return nil
}

// ValidateFor is an opt-in request/answer contract check for application policies.
// It rejects missing answers, mismatched kinds/options, bad distributions, and
// inconsistent scores. It is stricter than the upstream Python response decoder.
// Approximate probability sums and weighted scores use an absolute 1e-3 tolerance.
func (r *SystemOneResponse) ValidateFor(questions Questions) error {
	if r == nil {
		return invalidResponse("", "nil response")
	}
	normalized, err := normalizeQuestions(questions)
	if err != nil {
		return err
	}
	for name, raw := range normalized {
		var q map[string]json.RawMessage
		_ = json.Unmarshal(raw, &q)
		var kind string
		_ = json.Unmarshal(q["type"], &kind)
		path := "answers." + name
		a, ok := r.Answers[name]
		if !ok || a == nil {
			return invalidResponse(path, "required answer is missing or an unknown type")
		}
		if a.AnswerType() != kind {
			return invalidResponse(path+".type", "does not match question kind")
		}
		switch kind {
		case "noul":
			value, ok := r.Nouls[name]
			if !ok || !probabilityOK(value.Noul) {
				return invalidResponse(path, "invalid noul answer")
			}
		case "choice":
			value, ok := r.Choices[name]
			if !ok || !probabilityOK(value.Confidence) {
				return invalidResponse(path, "invalid choice answer")
			}
			var criteria map[string]json.RawMessage
			_ = json.Unmarshal(q["criteria"], &criteria)
			if len(criteria) == 0 || len(value.Probabilities) != len(criteria) {
				return invalidResponse(path+".probabilities", "option set does not match criteria")
			}
			selected, exists := value.Probabilities[value.Choice]
			if _, valid := criteria[value.Choice]; !exists || !valid {
				return invalidResponse(path+".choice", "selected label is not a requested option")
			}
			sum := 0.0
			for key := range criteria {
				p, exists := value.Probabilities[key]
				if !exists || !probabilityOK(p) {
					return invalidResponse(path+".probabilities", "missing or invalid probability")
				}
				if p > selected+1e-6 {
					return invalidResponse(path+".choice", "chosen option is not a maximum")
				}
				sum += p
			}
			if math.Abs(sum-1) > 1e-3 {
				return invalidResponse(path+".probabilities", "probabilities do not sum to one")
			}
		case "score":
			value, ok := r.Scores[name]
			if !ok || !probabilityOK(value.Confidence) {
				return invalidResponse(path, "invalid score answer")
			}
			var criteria []json.RawMessage
			_ = json.Unmarshal(q["criteria"], &criteria)
			if len(value.Probabilities) != len(criteria) || len(value.Legend) != len(criteria) {
				return invalidResponse(path, "score levels do not match criteria")
			}
			sum, weighted := 0.0, 0.0
			for i := range criteria {
				p, exists := value.Probabilities[i]
				if !exists || !probabilityOK(p) {
					return invalidResponse(path+".probabilities", "missing or invalid level probability")
				}
				if _, exists := value.Legend[i]; !exists {
					return invalidResponse(path+".legend", "missing level")
				}
				sum += p
				weighted += float64(i) * p
			}
			if math.Abs(sum-1) > 1e-3 {
				return invalidResponse(path+".probabilities", "probabilities do not sum to one")
			}
			// The service reports probabilities rounded to two decimals while the
			// score is a continuous estimate. Each level may be off by up to 0.005,
			// so the weighted value may shift by up to 0.005 * sum(level indexes).
			levels := len(criteria)
			tolerance := math.Max(1e-3, 0.005*float64(levels*(levels-1)/2))
			if math.IsNaN(value.Score) || math.IsInf(value.Score, 0) || math.Abs(value.Score-weighted) > tolerance {
				return invalidResponse(path+".score", "score is inconsistent with the distribution")
			}
		default:
			return invalidResponse(path, "unsupported answer kind for contract validation")
		}
	}
	return nil
}
