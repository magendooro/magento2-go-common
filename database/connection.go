// Package database provides a MySQL connection factory for Magento Go services.
//
// DSN parameters are fixed for Magento compatibility:
//   - parseTime=true — scan DATETIME/TIMESTAMP into time.Time
//   - charset=utf8mb4 — full Unicode (emoji-safe)
//   - loc=UTC — Go interprets timestamps as UTC
//   - time_zone=+00:00 — MySQL sends TIMESTAMP columns in UTC
package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const dsnParams = "parseTime=true&charset=utf8mb4&collation=utf8mb4_unicode_ci&loc=UTC&time_zone=%27%2B00%3A00%27"

// Config holds MySQL connection parameters common to all Magento Go services.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string

	// Socket is the Unix socket path used when Host == "localhost".
	// Falls back to /tmp/mysql.sock when empty.
	Socket string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// NewConnection opens and verifies a MySQL connection using cfg.
// When Host == "localhost", connects via Unix socket (Socket field or /tmp/mysql.sock).
// Otherwise connects via TCP.
func NewConnection(cfg Config) (*sql.DB, error) {
	var dsn string
	switch {
	case cfg.Host == "localhost" && cfg.Socket != "":
		dsn = fmt.Sprintf("%s:%s@unix(%s)/%s?%s",
			cfg.User, cfg.Password, cfg.Socket, cfg.Name, dsnParams)
	case cfg.Host == "localhost":
		dsn = fmt.Sprintf("%s:%s@unix(/tmp/mysql.sock)/%s?%s",
			cfg.User, cfg.Password, cfg.Name, dsnParams)
	default:
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?%s",
			cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Name, dsnParams)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
