package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/magendooro/magento2-go-common/cache"
)

const (
	maxRequestBodySize  = 2 << 20 // 2MB — oversized requests bypass cache; body is still forwarded intact
	defaultMaxRespSize  = 2 << 20 // 2MB — oversized responses are served but not stored in Redis
)

// CacheOptions configures which requests bypass the response cache.
type CacheOptions struct {
	// SkipAuthenticated skips caching when an Authorization header is present.
	// Enable for services with user-specific data (cart, customer).
	SkipAuthenticated bool

	// SkipMutations skips caching when the GraphQL operation type is "mutation".
	// Enable for services that support write operations.
	SkipMutations bool

	// MaxResponseSize is the maximum response body size (bytes) that will be
	// stored in Redis. Responses larger than this are served but not cached.
	// 0 uses the default of 2MB.
	MaxResponseSize int
}

// CacheMiddleware caches GraphQL POST responses in Redis.
// Only caches /graphql POST requests that return HTTP 200 with a valid,
// error-free JSON response body.
func CacheMiddleware(c *cache.Client, opts CacheOptions) func(http.Handler) http.Handler {
	maxRespSize := opts.MaxResponseSize
	if maxRespSize == 0 {
		maxRespSize = defaultMaxRespSize
	}

	return func(next http.Handler) http.Handler {
		if c == nil {
			return next // no-op when cache is unavailable
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
				next.ServeHTTP(w, r)
				return
			}

			if opts.SkipAuthenticated && r.Header.Get("Authorization") != "" {
				next.ServeHTTP(w, r)
				return
			}

			// Read one byte past the limit to detect oversized bodies without
			// truncating the handler's input.
			body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize+1))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			if len(body) > maxRequestBodySize {
				log.Debug().Int("bytes", len(body)).Msg("cache: request body exceeds 2MB, skipping cache")
				// Reconstruct the full body: we read maxRequestBodySize+1 bytes;
				// r.Body still holds the remainder.
				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
				next.ServeHTTP(w, r)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			if opts.SkipMutations && isMutation(body) {
				next.ServeHTTP(w, r)
				return
			}

			storeCode := r.Header.Get("Store")
			if storeCode == "" {
				storeCode = "default"
			}
			key := cache.CacheKey(storeCode, body)

			if cached, ok := c.Get(r.Context(), key); ok {
				log.Debug().Str("key", key).Msg("cache hit")
				w.Header().Set("Content-Type", "application/json")
				w.Write(cached)
				return
			}

			rec := &responseRecorder{ResponseWriter: w, body: &bytes.Buffer{}}
			next.ServeHTTP(rec, r)

			// Only cache successful, valid, error-free responses.
			statusOK := rec.statusCode == 0 || rec.statusCode == http.StatusOK
			if !statusOK {
				return
			}
			respBody := rec.body.Bytes()
			if len(respBody) > maxRespSize {
				log.Warn().Int("bytes", len(respBody)).Str("key", key).Msg("cache: response too large, skipping cache")
				return
			}
			if !isValidGraphQLResponse(respBody) {
				return
			}
			c.Set(r.Context(), key, respBody)
		})
	}
}

// isMutation reports whether the GraphQL request body is a mutation operation.
// Parses the "query" field and checks if its value starts with "mutation" after
// stripping leading whitespace — handles both inline and named operations.
func isMutation(body []byte) bool {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(req.Query), "mutation")
}

// isValidGraphQLResponse reports whether body is valid JSON and contains no
// GraphQL errors. Responses with an "errors" array are not cached — they are
// query-specific and often transient (auth failures, validation errors, etc.).
func isValidGraphQLResponse(body []byte) bool {
	if !json.Valid(body) {
		return false
	}
	var resp struct {
		Errors json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return false
	}
	// errors absent, null, or empty array → safe to cache
	s := strings.TrimSpace(string(resp.Errors))
	return s == "" || s == "null" || s == "[]"
}

type responseRecorder struct {
	http.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}
