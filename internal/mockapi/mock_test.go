package mockapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestMockProtocolBoundaries(t *testing.T) {
	server := New()
	defer server.Close()
	for _, tc := range []struct {
		method, path, body string
		status             int
		contains           string
	}{
		{"GET", "/missing", "", 404, "404"},
		{"POST", "/v1/systemone", "broken", 400, "invalid JSON"},
		{"POST", "/v1/systemone", `{"questions":{"q":{"type":"future"}}}`, 200, "future_fixture"},
		{"POST", "/v1/systemone", `{"questions":{"q":{"type":"choice","criteria":{}},"s":{"type":"score","criteria":[]}}}`, 200, "answers"},
	} {
		req, err := http.NewRequest(tc.method, server.URL+tc.path, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.status || !strings.Contains(string(raw), tc.contains) {
			t.Fatal(resp.StatusCode, string(raw))
		}
	}
}
