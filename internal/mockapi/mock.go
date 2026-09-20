// Package mockapi provides a local fixed-response test server, NOT an AI model.
package mockapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
)

func New() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-TypeSafe-Request-Id", "local-mock-request")
		if r.Method == http.MethodGet && r.URL.Path == "/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"models": []any{map[string]any{"name": "mock-model", "description": "Local fixed fixture; not a real model", "release_date": "2026-09-20"}}})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Questions map[string]struct {
				Type     string
				Criteria json.RawMessage
			} `json:"questions"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		answers := map[string]any{}
		for name, q := range req.Questions {
			switch q.Type {
			case "noul":
				answers[name] = map[string]any{"type": "noul", "noul": 0.02}
			case "choice":
				var criteria map[string]any
				_ = json.Unmarshal(q.Criteria, &criteria)
				chosen := ""
				for key := range criteria {
					if chosen == "" || key < chosen {
						chosen = key
					}
				}
				if _, ok := criteria["explain"]; ok {
					chosen = "explain"
				}
				probabilities := map[string]float64{}
				for key := range criteria {
					probabilities[key] = 0
				}
				if len(criteria) > 0 {
					probabilities[chosen] = 1
				}
				answers[name] = map[string]any{"type": "choice", "choice": chosen, "probabilities": probabilities, "confidence": 1}
			case "score":
				var criteria []any
				_ = json.Unmarshal(q.Criteria, &criteria)
				legend := map[int]any{}
				probabilities := map[int]float64{}
				for i, v := range criteria {
					legend[i] = v
					probabilities[i] = 0
				}
				if len(criteria) > 0 {
					probabilities[0] = 1
				}
				answers[name] = map[string]any{"type": "score", "score": 0, "legend": legend, "probabilities": probabilities, "confidence": 1}
			default:
				answers[name] = map[string]any{"type": "future_fixture", "value": true}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "mock-model", "answers": answers, "usage": map[string]any{"input_tokens": 0, "output_tokens": 0}})
	}))
}
