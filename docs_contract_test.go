package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"testing"
)

type contractCase struct {
	Name    string `json:"name"`
	Method  string `json:"method"`
	Typed   bool   `json:"typed"`
	Model   string `json:"model"`
	Request struct {
		State     any                       `json:"state"`
		Questions map[string]map[string]any `json:"questions"`
	} `json:"request"`
	ExtraBody        map[string]any  `json:"extra_body"`
	Response         json.RawMessage `json:"response"`
	ExpectedRequest  json.RawMessage `json:"expected_request"`
	ExpectedResponse json.RawMessage `json:"expected_response"`
}

func contractJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
func TestOfficialSDKCorpus(t *testing.T) {
	raw, err := os.ReadFile("testdata/docs-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []contractCase
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&cases); err != nil {
		t.Fatal(err)
	}
	results := map[string]any{}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			var wire any
			c, err := NewClient(Config{APIKey: "fixture-key", Retry: &RetryPolicy{}, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Fatal("auth header")
				}
				if tc.Method == "GET" {
					if r.Method != "GET" || r.URL.Path != "/v1/models" {
						t.Fatal("models endpoint")
					}
				} else {
					if r.Method != "POST" || r.URL.Path != "/v1/systemone" {
						t.Fatal("evaluation endpoint")
					}
					requestBytes, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
					wire = contractJSON(t, requestBytes)
					if !reflect.DeepEqual(wire, contractJSON(t, tc.ExpectedRequest)) {
						t.Fatalf("wire contract changed: %s", requestBytes)
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{RequestIDHeader: []string{"fixture-request"}}, Body: io.NopCloser(bytes.NewReader(tc.Response))}, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			var result any
			if tc.Method == "GET" {
				result, err = c.ListModels(context.Background())
			} else {
				qs := Questions{}
				for name, q := range tc.Request.Questions {
					if !tc.Typed {
						qs[name] = RawQuestion(q)
						continue
					}
					switch q["type"] {
					case "noul":
						var criteria NoulCriteria
						if q["criteria"] != nil {
							criteria = NoulCriteria(q["criteria"].(map[string]any))
						}
						qs[name] = Noul{Instructions: q["instructions"], Criteria: criteria}
					case "choice":
						qs[name] = Choice{Instructions: q["instructions"], Criteria: q["criteria"].(map[string]any)}
					case "score":
						qs[name] = Score{Instructions: q["instructions"], Criteria: q["criteria"].([]any)}
					default:
						t.Fatal("unsupported fixture kind")
					}
				}
				result, err = c.SystemOne(context.Background(), SystemOneRequest{State: tc.Request.State, Questions: qs, Model: tc.Model, ExtraBody: tc.ExtraBody})
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			response := contractJSON(t, encoded)
			if !reflect.DeepEqual(response, contractJSON(t, tc.ExpectedResponse)) {
				t.Fatalf("response contract changed: %s", encoded)
			}
			results[tc.Name] = map[string]any{"request": wire, "response": response}
		})
	}
	// Optional evidence for the separate pinned-Python differential CI job.
	if path := os.Getenv("TYPESAFE_PARITY_OUTPUT"); path != "" {
		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
