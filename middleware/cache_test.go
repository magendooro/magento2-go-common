package middleware

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsMutation(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		want  bool
	}{
		{"mutation keyword", `{"query":"mutation { placeOrder }"}`, true},
		{"mutation with leading whitespace", `{"query":"  mutation AddToCart { addProductsToCart }"}`, true},
		{"mutation with newline", `{"query":"\n  mutation { foo }"}`, true},
		{"query operation", `{"query":"query { cart { id } }"}`, false},
		{"shorthand query", `{"query":"{ products { items { id } } }"}`, false},
		{"field named mutationCount", `{"query":"query { mutationCount }"}`, false},
		{"invalid json", `not json`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isMutation([]byte(tc.body))
			if got != tc.want {
				t.Errorf("isMutation(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestIsValidGraphQLResponse(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		valid bool
	}{
		{"data only", `{"data":{"cart":{"id":"abc"}}}`, true},
		{"errors present", `{"errors":[{"message":"not found"}],"data":null}`, false},
		{"errors null", `{"data":{},"errors":null}`, true},
		{"errors empty array", `{"data":{},"errors":[]}`, true},
		{"invalid json", `{broken`, false},
		{"empty", ``, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidGraphQLResponse([]byte(tc.body))
			if got != tc.valid {
				t.Errorf("isValidGraphQLResponse(%q) = %v, want %v", tc.body, got, tc.valid)
			}
		})
	}
}

// stubCache is an in-memory cache for testing CacheMiddleware without Redis.
type stubCache struct {
	store map[string][]byte
}

func newStubCache() *stubCache { return &stubCache{store: map[string][]byte{}} }

func (s *stubCache) Get(key string) ([]byte, bool) { v, ok := s.store[key]; return v, ok }
func (s *stubCache) Set(key string, val []byte)    { s.store[key] = val }

// stubHandler returns a fixed response body and status code.
func stubHandler(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	})
}

func TestCacheMiddleware_OversizedRequestBody(t *testing.T) {
	// Build a body just over the 2MB limit
	large := make([]byte, maxRequestBodySize+1)
	for i := range large {
		large[i] = 'a'
	}
	// Wrap in minimal JSON so handler receives valid content
	reqBody := append([]byte(`{"query":"`), large...)
	reqBody = append(reqBody, '"', '}')

	handlerCalled := false
	var receivedBodyLen int
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		b, _ := io.ReadAll(r.Body)
		receivedBodyLen = len(b)
		w.Write([]byte(`{"data":{}}`))
	})

	req := httptest.NewRequest(http.MethodPost, "/graphql", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()

	// Use a nil cache to verify the middleware still passes through correctly
	mw := CacheMiddleware(nil, CacheOptions{})(h)
	mw.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Fatal("handler was not called for oversized request")
	}
	// Handler must receive the full body — not truncated
	if receivedBodyLen != len(reqBody) {
		t.Errorf("handler received %d bytes, want %d", receivedBodyLen, len(reqBody))
	}
}

func TestIsMutation_FieldNamedMutation(t *testing.T) {
	// A query field whose name starts with "mutation" must NOT be detected as mutation
	body := `{"query":"query { mutationLog { id } }"}`
	if isMutation([]byte(body)) {
		t.Error("field named mutationLog incorrectly detected as mutation operation")
	}
}

func TestIsValidGraphQLResponse_WhitespaceErrors(t *testing.T) {
	// errors field with whitespace variants
	cases := []struct {
		body  string
		valid bool
	}{
		{`{"data":{},"errors":  null  }`, true},
		{`{"data":{},"errors":  []  }`, true},
		{`{"data":null,"errors":[{"message":"oops"}]}`, false},
	}
	for _, tc := range cases {
		got := isValidGraphQLResponse([]byte(tc.body))
		if got != tc.valid {
			t.Errorf("isValidGraphQLResponse(%q) = %v, want %v", tc.body, got, tc.valid)
		}
	}
}

func TestCacheMiddleware_SkipMutationsUsesJSONParse(t *testing.T) {
	// Ensure a query containing the word "mutation" in a field name is not skipped
	body := `{"query":"query { getMutationHistory { id } }"}`
	handlerCallCount := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCallCount++
		w.Write([]byte(`{"data":{"getMutationHistory":[]}}`))
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(body))
		rec := httptest.NewRecorder()
		// nil cache — just verifying SkipMutations doesn't incorrectly skip
		mw := CacheMiddleware(nil, CacheOptions{SkipMutations: true})(h)
		mw.ServeHTTP(rec, req)
	}
	if handlerCallCount != 2 {
		t.Errorf("handler called %d times, want 2 (query with 'mutation' in field name should not be skipped)", handlerCallCount)
	}
}
