package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// newTestServer creates an httptest.Server that responds to /chat/completions
// with the given content. The content is JSON-escaped so newlines and quotes
// in the content do not break the response. The caller must close the server
// via the returned cleanup function.
func newTestServer(t *testing.T, content string) (*httptest.Server, string) {
	t.Helper()
	escaped, _ := json.Marshal(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":` + string(escaped) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, srv.URL
}

// newModelServer creates an httptest.Server that records the model name sent
// in the request body and responds with the given content.
func newModelServer(t *testing.T, content string) (*httptest.Server, string, *string) {
	t.Helper()
	var gotModel string
	escaped, _ := json.Marshal(content)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":` + string(escaped) + `}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv, srv.URL, &gotModel
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	t.Parallel()
	c := New("http://example.com/v1/", "", "")
	if c.baseURL != "http://example.com/v1" {
		t.Errorf("baseURL = %q, want http://example.com/v1", c.baseURL)
	}
}

func TestNewTrimsMultipleSlashes(t *testing.T) {
	t.Parallel()
	c := New("http://example.com///", "", "")
	if c.baseURL != "http://example.com" {
		t.Errorf("baseURL = %q, want http://example.com", c.baseURL)
	}
}

func TestNewEmptyModelDefaults(t *testing.T) {
	t.Parallel()
	c := New("http://x", "", "")
	if c.model != DefaultModel {
		t.Errorf("model = %q, want %q", c.model, DefaultModel)
	}
}

func TestNewExplicitModelPreserved(t *testing.T) {
	t.Parallel()
	c := New("http://x", "custom-model", "")
	if c.model != "custom-model" {
		t.Errorf("model = %q, want custom-model", c.model)
	}
}

func TestAvailableProbesModelsEndpoint(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "", "")
	if !c.Available(context.Background()) {
		t.Fatal("expected available=true")
	}
	if gotPath != "/models" {
		t.Errorf("probed path = %q, want /models", gotPath)
	}
}

func TestAvailableReturnsFalseOn500(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "", "")
	if c.Available(context.Background()) {
		t.Fatal("expected available=false for 500 response")
	}
}

func TestAvailableReturnsFalseOn404(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "", "")
	if c.Available(context.Background()) {
		t.Fatal("expected available=false for 404 response")
	}
}

func TestAvailableNoTokenSendsNoAuth(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "", "")
	_ = c.Available(context.Background())
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty when no token set", gotAuth)
	}
}

func TestAvailableWithTokenSendsBearer(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "", "my-token")
	_ = c.Available(context.Background())
	if gotAuth != "Bearer my-token" {
		t.Errorf("Authorization = %q, want Bearer my-token", gotAuth)
	}
}

func TestDescribeDetailSendsModel(t *testing.T) {
	t.Parallel()
	_, url, gotModel := newModelServer(t, "ok")

	c := New(url, "my-model", "")
	if _, err := c.DescribeDetail(context.Background(), "diff", "stat"); err != nil {
		t.Fatal(err)
	}
	if *gotModel != "my-model" {
		t.Errorf("model sent = %q, want my-model", *gotModel)
	}
}

func TestDescribeDetailSendsContentJSON(t *testing.T) {
	t.Parallel()
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "")
	if _, err := c.DescribeDetail(context.Background(), "d", "s"); err != nil {
		t.Fatal(err)
	}
	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
}

func TestDescribeDetailTrimsWhitespace(t *testing.T) {
	t.Parallel()
	_, url := newTestServer(t, "  feat: add thing  ")

	c := New(url, "m", "")
	out, err := c.DescribeDetail(context.Background(), "d", "s")
	if err != nil {
		t.Fatal(err)
	}
	if out != "feat: add thing" {
		t.Errorf("output = %q, want trimmed content", out)
	}
}

func TestSummarizeTLDRSendsModel(t *testing.T) {
	t.Parallel()
	_, url, gotModel := newModelServer(t, "ok")

	c := New(url, "tldr-model", "")
	if _, err := c.SummarizeTLDR(context.Background(), "detail"); err != nil {
		t.Fatal(err)
	}
	if *gotModel != "tldr-model" {
		t.Errorf("model sent = %q, want tldr-model", *gotModel)
	}
}

func TestSummarizeTLDRSendsBearerToken(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "bearer-tok")
	if _, err := c.SummarizeTLDR(context.Background(), "detail"); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer bearer-tok" {
		t.Errorf("Authorization = %q, want Bearer bearer-tok", gotAuth)
	}
}

func TestSummarizeTLDRTrimsOutput(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Response with leading/trailing whitespace that the client must trim.
		w.Write([]byte(`{"choices":[{"message":{"content":"  Add thing  "}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "")
	out, err := c.SummarizeTLDR(context.Background(), "detail")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Add thing" {
		t.Errorf("output = %q, want Add thing", out)
	}
}

func TestChatReturnsErrorOn500(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server error"}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "")
	_, err := c.DescribeDetail(context.Background(), "d", "s")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention 500: %v", err)
	}
}

func TestChatReturnsErrorOnMalformedJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json at all`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "")
	_, err := c.SummarizeTLDR(context.Background(), "detail")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parse: %v", err)
	}
}

func TestChatReturnsErrorOnEmptyChoices(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "")
	_, err := c.DescribeDetail(context.Background(), "d", "s")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error should mention no choices: %v", err)
	}
}

func TestChatReturnsErrorOn403(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "bad-token")
	_, err := c.SummarizeTLDR(context.Background(), "detail")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("error should mention 403: %v", err)
	}
}

func TestAvailableConcurrentSafe(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "", "")
	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			if !c.Available(context.Background()) {
				t.Errorf("expected available=true")
			}
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if callCount != n {
		t.Errorf("callCount = %d, want %d", callCount, n)
	}
}

func TestDescribeDetailSendsTemperature(t *testing.T) {
	t.Parallel()
	var gotTemp float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Temperature float64 `json:"temperature"`
		}
		_ = json.Unmarshal(body, &req)
		gotTemp = req.Temperature
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "")
	if _, err := c.DescribeDetail(context.Background(), "d", "s"); err != nil {
		t.Fatal(err)
	}
	if gotTemp != 0.3 {
		t.Errorf("temperature = %v, want 0.3", gotTemp)
	}
}

func TestChatSendsSingleUserMessage(t *testing.T) {
	t.Parallel()
	var msgCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		msgCount = len(req.Messages)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL, "m", "")
	if _, err := c.DescribeDetail(context.Background(), "d", "s"); err != nil {
		t.Fatal(err)
	}
	if msgCount != 1 {
		t.Errorf("message count = %d, want 1", msgCount)
	}
}

func TestBaseURLUsedForChatCompletions(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL+"/v1/", "m", "")
	if _, err := c.SummarizeTLDR(context.Background(), "detail"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Errorf("path = %q, want /v1/chat/completions", gotPath)
	}
}
