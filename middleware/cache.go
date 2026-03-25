package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/magendooro/magento2-go-common/cache"
)

// CacheOptions configures which requests bypass the response cache.
type CacheOptions struct {
	// SkipAuthenticated skips caching when an Authorization header is present.
	// Enable for services with user-specific data (cart, customer).
	SkipAuthenticated bool

	// SkipMutations skips caching when the request body contains "mutation".
	// Enable for services that support write operations.
	SkipMutations bool
}

// CacheMiddleware caches GraphQL POST responses in Redis.
// Only caches /graphql POST requests that return HTTP 200.
// Pass opts to control which requests are excluded from caching.
func CacheMiddleware(c *cache.Client, opts CacheOptions) func(http.Handler) http.Handler {
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

			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))

			if opts.SkipMutations && bytes.Contains(body, []byte("mutation")) {
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
				w.Header().Set("X-Cache", "HIT")
				w.Write(cached)
				return
			}

			rec := &responseRecorder{ResponseWriter: w, body: &bytes.Buffer{}}
			next.ServeHTTP(rec, r)

			if rec.statusCode == 0 || rec.statusCode == http.StatusOK {
				c.Set(r.Context(), key, rec.body.Bytes())
				w.Header().Set("X-Cache", "MISS")
			}
		})
	}
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
