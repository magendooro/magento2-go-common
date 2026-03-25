package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"sync"

	"github.com/rs/zerolog/log"
)

// contextKey is an unexported type for context keys in this package.
// Prevents collisions with keys from other packages.
type contextKey string

const storeIDKey contextKey = "store_id"

// StoreResolver resolves a Magento store code to its integer store_id.
// Results are cached in memory after the first lookup.
type StoreResolver struct {
	db    *sql.DB
	cache map[string]int
	mu    sync.RWMutex
}

// NewStoreResolver creates a StoreResolver backed by the given DB connection.
func NewStoreResolver(db *sql.DB) *StoreResolver {
	return &StoreResolver{
		db:    db,
		cache: make(map[string]int),
	}
}

// Resolve returns the store_id for a store code, querying the DB on first access.
func (sr *StoreResolver) Resolve(code string) (int, error) {
	sr.mu.RLock()
	if id, ok := sr.cache[code]; ok {
		sr.mu.RUnlock()
		return id, nil
	}
	sr.mu.RUnlock()

	var storeID int
	err := sr.db.QueryRow(
		"SELECT store_id FROM store WHERE code = ? AND is_active = 1", code,
	).Scan(&storeID)
	if err != nil {
		return 0, err
	}

	sr.mu.Lock()
	sr.cache[code] = storeID
	sr.mu.Unlock()

	return storeID, nil
}

// StoreMiddleware extracts the Store header, resolves it to a store_id, and
// stores the result in the request context. Defaults to store code "default".
func StoreMiddleware(resolver *StoreResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			storeCode := r.Header.Get("Store")
			if storeCode == "" {
				storeCode = "default"
			}

			storeID, err := resolver.Resolve(storeCode)
			if err != nil {
				log.Warn().Str("store_code", storeCode).Err(err).Msg("Failed to resolve store, using default (0)")
				storeID = 0
			}

			ctx := context.WithValue(r.Context(), storeIDKey, storeID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetStoreID returns the store_id from context, or 0 if absent.
func GetStoreID(ctx context.Context) int {
	if id, ok := ctx.Value(storeIDKey).(int); ok {
		return id
	}
	return 0
}
