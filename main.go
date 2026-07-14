package main

import (
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/alexedwards/scs/v2"
)

type App struct {
	templates      *template.Template
	logger         *slog.Logger
	sessionManager *scs.SessionManager
	proxyAuth      ProxyAuthConfig

	ldap                   *ldapDAO
	resolveProxyAuthUserDN func(string) (string, error)
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	config := loadConfig()

	tmpl, err := newTemplates()
	if err != nil {
		logger.Error("parse templates failed", "error", err)
		os.Exit(1)
	}

	sessionManager, err := newSessionManager(logger, config.Session)
	if err != nil {
		logger.Error("initialize session manager failed", "error", err)
		os.Exit(1)
	}

	ldap := &ldapDAO{config: config.LDAP, logger: logger}
	app := &App{
		templates:      tmpl,
		logger:         logger,
		sessionManager: sessionManager,
		proxyAuth:      config.ProxyAuth,

		ldap:                   ldap,
		resolveProxyAuthUserDN: ldap.findUserDN,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /", app.redirectRoot)

	mux.HandleFunc("GET /login", app.getLogin)
	mux.HandleFunc("POST /login", app.postLogin)

	mux.HandleFunc("GET /profile", app.requireLogin(app.showProfile))
	mux.HandleFunc("POST /profile", app.requireLogin(app.postProfile))

	mux.HandleFunc("POST /logout", app.logout)

	staticSubFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		logger.Error("create static fs failed", "error", err)
		os.Exit(1)
	}

	mux.Handle(
		"GET /static/",
		http.StripPrefix("/static/", http.FileServer(http.FS(staticSubFS))),
	)

	srv := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           app.sessionManager.LoadAndSave(app.logRequest(app.authenticateProxyAuth(mux))),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("adprofd listening", "addr", srv.Addr)

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("listen and serve failed", "error", err)
		os.Exit(1)
	}
}

// END: SESSION
