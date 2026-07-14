package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	_ "github.com/mattn/go-sqlite3"
)

func newSessionManager(logger *slog.Logger, config SessionConfig) (*scs.SessionManager, error) {
	db, err := sql.Open("sqlite3", config.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open session database %q: %w", config.DBPath, err)
	}

	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
	token TEXT PRIMARY KEY,
	data BLOB NOT NULL,
	expiry REAL NOT NULL
);

CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expiry);
`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialize session database %q: %w", config.DBPath, err)
	}

	sessionManager := scs.New()
	sessionManager.Store = sqlite3store.New(db)
	sessionManager.Lifetime = 8 * time.Hour
	sessionManager.IdleTimeout = 30 * time.Minute

	sessionManager.Cookie.Name = "ADPROFDSESSION"
	sessionManager.Cookie.HttpOnly = true
	sessionManager.Cookie.SameSite = http.SameSiteLaxMode
	sessionManager.Cookie.Secure = config.CookieSecure

	logger.Info("session manager initialized",
		"db", config.DBPath,
		"cookie_secure", config.CookieSecure,
	)

	return sessionManager, nil
}
