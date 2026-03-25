package middleware

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/magendooro/magento2-go-common/jwt"
)

const customerIDKey contextKey = "customer_id"
const bearerTokenKey contextKey = "bearer_token"

// TokenResolver validates bearer tokens against JWT and the legacy oauth_token table.
type TokenResolver struct {
	db         *sql.DB
	jwtManager *jwt.Manager
}

// NewTokenResolver creates a TokenResolver. jwtManager may be nil to disable JWT
// validation and fall back to oauth_token only.
func NewTokenResolver(db *sql.DB, jwtManager *jwt.Manager) *TokenResolver {
	return &TokenResolver{db: db, jwtManager: jwtManager}
}

// Resolve returns the customer_id for a Bearer token.
// Tries JWT first (Magento 2.4+ default), then falls back to oauth_token (legacy).
func (tr *TokenResolver) Resolve(token string) (int, error) {
	if tr.jwtManager != nil {
		customerID, err := tr.jwtManager.Validate(token)
		if err == nil {
			revoked, err := tr.isJWTRevoked(customerID, token)
			if err != nil {
				log.Debug().Err(err).Msg("jwt revocation check failed")
			}
			if !revoked {
				return customerID, nil
			}
			log.Debug().Int("customer_id", customerID).Msg("jwt token revoked")
			return 0, fmt.Errorf("token has been revoked")
		}
		log.Debug().Err(err).Msg("jwt validation failed, trying oauth_token")
	}

	var customerID int
	err := tr.db.QueryRow(
		`SELECT customer_id FROM oauth_token
		 WHERE token = ? AND revoked = 0 AND customer_id IS NOT NULL`,
		token,
	).Scan(&customerID)
	if err != nil {
		return 0, err
	}
	return customerID, nil
}

func (tr *TokenResolver) isJWTRevoked(customerID int, tokenString string) (bool, error) {
	var revokeBefore int64
	err := tr.db.QueryRow(
		"SELECT revoke_before FROM jwt_auth_revoked WHERE user_type_id = ? AND user_id = ?",
		jwt.CustomerUserType, customerID,
	).Scan(&revokeBefore)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	iat, err := tr.jwtManager.GetIssuedAt(tokenString)
	if err != nil {
		return true, nil // can't parse iat — treat as revoked
	}
	return iat.Unix() <= revokeBefore, nil
}

// RevokeJWT writes a revocation record so the customer's current token is invalidated.
func (tr *TokenResolver) RevokeJWT(customerID int) error {
	_, err := tr.db.Exec(
		`INSERT INTO jwt_auth_revoked (user_type_id, user_id, revoke_before)
		 VALUES (?, ?, ?)
		 ON DUPLICATE KEY UPDATE revoke_before = VALUES(revoke_before)`,
		jwt.CustomerUserType, customerID, time.Now().Unix(),
	)
	return err
}

// AuthMiddleware extracts the Bearer token from Authorization header, resolves it
// to a customer_id, and stores both in the request context.
func AuthMiddleware(resolver *TokenResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var customerID int

			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				id, err := resolver.Resolve(token)
				if err != nil {
					log.Debug().Err(err).Msg("token resolution failed")
				} else {
					customerID = id
				}
			}

			ctx := context.WithValue(r.Context(), customerIDKey, customerID)
			if strings.HasPrefix(authHeader, "Bearer ") {
				ctx = context.WithValue(ctx, bearerTokenKey, strings.TrimPrefix(authHeader, "Bearer "))
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetCustomerID returns the authenticated customer ID from context, or 0 if unauthenticated.
func GetCustomerID(ctx context.Context) int {
	if id, ok := ctx.Value(customerIDKey).(int); ok {
		return id
	}
	return 0
}

// GetBearerToken returns the raw Bearer token from context, or empty string.
func GetBearerToken(ctx context.Context) string {
	if t, ok := ctx.Value(bearerTokenKey).(string); ok {
		return t
	}
	return ""
}
