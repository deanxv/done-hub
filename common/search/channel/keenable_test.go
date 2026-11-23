package channel

import (
	"done-hub/common/requester"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Query goes through the shared requester client, which main() normally sets up.
	if requester.HTTPClient == nil {
		requester.HTTPClient = &http.Client{}
	}
	os.Exit(m.Run())
}

func TestKeenableQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/search" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Errorf("X-API-Key = %q, want test-key", got)
		}
		var body KeenableRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Query != "go generics" {
			t.Errorf("unexpected body %+v (%v)", body, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"A","url":"https://a.example","description":"desc A","snippet":"long A"},
			{"title":"B","url":"https://b.example","description":"","snippet":"snippet B"},
			{"title":"no url","url":""}
		]}`))
	}))
	defer server.Close()

	resp, err := NewKeenable("test-key", server.URL+"/").Query("go generics")
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("got %d results, want 2", len(resp.Results))
	}
	if resp.Results[0].Content != "desc A" || resp.Results[0].Url != "https://a.example" {
		t.Errorf("first result = %+v", resp.Results[0])
	}
	if resp.Results[1].Content != "snippet B" {
		t.Errorf("second result should fall back to snippet, got %+v", resp.Results[1])
	}
}

func TestKeenableQueryError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid api key"}`))
	}))
	defer server.Close()

	if _, err := NewKeenable("bad", server.URL).Query("q"); err == nil {
		t.Fatal("expected an error for a 401 response")
	}
}

func TestNewKeenableDefaultBaseURL(t *testing.T) {
	if got := NewKeenable("k", "").baseURL; got != keenableDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", got, keenableDefaultBaseURL)
	}
}
