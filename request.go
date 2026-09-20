package typesafe

import (
	"bytes"
	"encoding/json"
	"strings"
)

func contentOK(raw json.RawMessage, nullable bool) bool {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return false
	}
	if bytes.Equal(raw, []byte("null")) {
		return nullable
	}
	return json.Valid(raw) && (raw[0] == '"' || raw[0] == '{' || raw[0] == '[')
}

func normalizeQuestions(qs Questions) (map[string]json.RawMessage, error) {
	if len(qs) == 0 {
		return nil, invalid("questions", "at least one question is required")
	}
	out := make(map[string]json.RawMessage, len(qs))
	for name, q := range qs {
		path := "questions." + name
		raw, err := json.Marshal(q)
		if err != nil {
			return nil, invalid(path, "cannot encode question as JSON")
		}
		if err := validateQuestion(raw, path); err != nil {
			return nil, err
		}
		out[name] = raw
	}
	return out, nil
}

func validateQuestion(raw []byte, path string) error {
	var q map[string]json.RawMessage
	if err := json.Unmarshal(raw, &q); err != nil || q == nil {
		return invalid(path, "expected a question object")
	}
	var kind string
	if err := json.Unmarshal(q["type"], &kind); err != nil || strings.TrimSpace(kind) == "" {
		return invalid(path+".type", "expected a nonempty string")
	}
	if instr, exists := q["instructions"]; exists && !contentOK(instr, true) {
		return invalid(path+".instructions", "expected text, object, array, or null")
	}
	switch kind {
	case "choice", "noul":
		rawCriteria, exists := q["criteria"]
		if kind == "noul" && (!exists || bytes.Equal(bytes.TrimSpace(rawCriteria), []byte("null"))) {
			return nil
		}
		var criteria map[string]json.RawMessage
		if !exists || json.Unmarshal(rawCriteria, &criteria) != nil || criteria == nil {
			return invalid(path+".criteria", "expected an object")
		}
		for name, value := range criteria {
			if kind == "noul" && name != "true" && name != "false" {
				return invalid(path+".criteria", "noul criteria keys must be true or false")
			}
			if !contentOK(value, true) {
				return invalid(path+".criteria."+name, "expected text, object, array, or null")
			}
		}
	case "score":
		var criteria []json.RawMessage
		if json.Unmarshal(q["criteria"], &criteria) != nil || len(criteria) == 0 {
			return invalid(path+".criteria", "expected a nonempty ordered array")
		}
		for _, value := range criteria {
			if !contentOK(value, false) {
				return invalid(path+".criteria", "levels must be text, objects, or arrays")
			}
		}
	}
	return nil
}

func prepareBody(req SystemOneRequest, model string) ([]byte, error) {
	if req.Model != "" {
		model = req.Model
	}
	state, err := json.Marshal(req.State)
	if err != nil || !contentOK(state, false) {
		return nil, invalid("state", "expected JSON text, object, or array")
	}
	qs, err := normalizeQuestions(req.Questions)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"state": json.RawMessage(state), "model": model, "questions": qs}
	for key, value := range req.ExtraBody {
		body[key] = value
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, invalid("body", "cannot encode JSON")
	}
	return raw, nil
}
