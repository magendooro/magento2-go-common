// Package cache provides a Redis-backed response cache for Magento GraphQL services.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

// Client wraps a Redis client with prefix-scoped caching helpers.
type Client struct {
	rdb    *redis.Client
	prefix string
	ttl    time.Duration
}

// Config holds Redis connection and caching configuration.
type Config struct {
	Host     string
	Port     string
	Password string
	DB       int
	TTL      time.Duration

	// Prefix namespaces all keys for this service (e.g. "gql:" or "cust_gql:").
	// Required — prevents key collisions between services sharing a Redis instance.
	Prefix string
}

// New creates a Redis cache client. Returns nil if the connection fails so callers
// can treat cache as optional without extra nil checks.
func New(cfg Config) *Client {
	if cfg.TTL == 0 {
		cfg.TTL = 5 * time.Minute
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:        fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Password:    cfg.Password,
		DB:          cfg.DB,
		DialTimeout: 5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Warn().Err(err).Msg("Redis connection failed, caching disabled")
		return nil
	}

	log.Info().
		Str("addr", fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)).
		Int("db", cfg.DB).
		Str("prefix", cfg.Prefix).
		Msg("Redis cache connected")

	return &Client{
		rdb:    rdb,
		prefix: cfg.Prefix,
		ttl:    cfg.TTL,
	}
}

// CacheKey builds a 16-char hex cache key from the store code and request body.
func CacheKey(storeCode string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(storeCode))
	h.Write([]byte(":"))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Get retrieves a cached response. Returns (nil, false) on miss or error.
func (c *Client) Get(ctx context.Context, key string) ([]byte, bool) {
	val, err := c.rdb.Get(ctx, c.prefix+key).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

// Set stores a response in cache.
func (c *Client) Set(ctx context.Context, key string, data []byte) {
	if err := c.rdb.Set(ctx, c.prefix+key, data, c.ttl).Err(); err != nil {
		log.Warn().Err(err).Str("key", key).Msg("cache set failed")
	}
}

// Flush removes all entries with this client's prefix.
func (c *Client) Flush(ctx context.Context) error {
	iter := c.rdb.Scan(ctx, 0, c.prefix+"*", 100).Iterator()
	for iter.Next(ctx) {
		c.rdb.Del(ctx, iter.Val())
	}
	return iter.Err()
}

// Close closes the underlying Redis connection.
func (c *Client) Close() error {
	return c.rdb.Close()
}
