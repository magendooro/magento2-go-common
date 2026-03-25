# CLAUDE.md — magento2-go-common

Shared Go packages used by all three Magento 2 GraphQL microservices (catalog, cart, customer). Lives in the same `go.work` workspace as the services.

**Module path**: `github.com/magendooro/magento2-go-common`

## Packages

### `config` — ConfigProvider

Hierarchical scope resolution over `core_config_data`. Reads all config at startup into memory; no per-request DB queries.

```go
cp, err := config.NewConfigProvider(db)
cp.Get("tax/calculation/price_includes_tax", storeID)   // string
cp.GetBool("tax/calculation/price_includes_tax", storeID)
cp.GetInt("catalog/layered_navigation/price_range_step", storeID, 10)
cp.GetFloat("tax/defaults/rate", storeID, 0)
cp.GetWebsiteID(storeID)
```

Scope resolution order: store → website → default. Falls back gracefully — missing keys return `""`.

### `jwt` — Magento-compatible JWT

HS256 JWTs with `kid: "1"`. Signing key derived from Magento's `crypt/key` via `STR_PAD_BOTH` to 2048 chars with `&`.

```go
mgr := jwt.NewManager(cryptKey, ttlMinutes)
token, err := mgr.Create(customerID)
customerID, err := mgr.Validate(token)
iat, err := mgr.GetIssuedAt(token)
```

Cross-compatible with Magento PHP: Go tokens validate in PHP, PHP tokens validate in Go.

### `cache` — Redis client

```go
c := cache.New(cache.Config{
    Host:     "localhost",
    Port:     "6379",
    Password: "",
    DB:       0,
    Prefix:   "gql:",  // per-service prefix to avoid cross-service collisions
})
c.Get(ctx, key)
c.Set(ctx, key, value, ttl)
c.Flush(ctx)
c.Close()
```

### `database` — MySQL connection

```go
db, err := database.NewConnection(database.Config{
    Host:            "localhost",  // triggers Unix socket: /tmp/mysql.sock
    Port:            "3306",
    User:            "fch",
    Password:        "",
    Name:            "magento248",
    Socket:          "",           // override socket path if needed
    MaxOpenConns:    10,
    MaxIdleConns:    5,
    ConnMaxLifetime: 5 * time.Minute,
    ConnMaxIdleTime: 2 * time.Minute,
})
```

When `Host == "localhost"` and `Socket == ""`, connects via `/tmp/mysql.sock`. DSN always includes `parseTime=true&time_zone=UTC&loc=UTC`.

### `middleware` — HTTP middleware chain

**Functions:**

```go
middleware.CORSMiddleware(h)
middleware.LoggingMiddleware(h)
middleware.RecoveryMiddleware(h)
middleware.StoreMiddleware(storeResolver)(h)
middleware.AuthMiddleware(tokenResolver)(h)
middleware.CacheMiddleware(cacheClient, middleware.CacheOptions{
    SkipAuthenticated: true,
    SkipMutations:     true,
})(h)
```

**Context accessors:**

```go
middleware.GetStoreID(ctx)       // int  — 0 if no store header
middleware.GetCustomerID(ctx)    // int  — 0 if unauthenticated
middleware.GetBearerToken(ctx)   // string — raw Authorization Bearer value
```

**Types:**

```go
middleware.NewStoreResolver(db)
middleware.NewTokenResolver(db, jwtManager)   // jwtManager may be nil (oauth-only mode)
middleware.TokenResolver                      // embed in service Resolver struct
```

**Context key internals:** Uses unexported `type contextKey string`. Never export these — exported context keys cause collisions when packages share a context.

## Build & Test

```bash
# From workspace root
GOTOOLCHAIN=auto go build github.com/magendooro/magento2-go-common/...
GOTOOLCHAIN=auto go vet github.com/magendooro/magento2-go-common/...
GOTOOLCHAIN=auto go test github.com/magendooro/magento2-go-common/... -v -count=1
```

Tests are unit tests only — no DB or Redis required. 13 tests covering ConfigProvider scope resolution and JWT round-trips.

## Rules for extending go-common

1. **Only add code that is used by two or more services.** Single-service logic stays in that service.
2. **No service-specific types or imports** — common packages must not import any `magento2-*-graphql-go` package.
3. **No business logic** — common holds infrastructure (connectivity, auth, config, HTTP plumbing). Domain logic (tax calculation, cart totals, order placement) belongs in the service.
4. **Exported context keys are forbidden** — use unexported `type contextKey string` always.
5. **Add unit tests** for new packages. Tests that need DB/Redis must be in the service's `tests/` package instead.

## Versioning

The module uses git on `master` branch. It is resolved via the workspace `go.work` `use` directive — **no version tag is needed for local development**. Do not add `require github.com/magendooro/magento2-go-common` to any service's `go.mod`; the `use` directive handles it.

If this module is ever published for standalone use, tag with `v0.1.0` before adding `require` directives elsewhere.
