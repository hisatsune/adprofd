package main

import (
	"log/slog"
	"net/http"
	"time"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}

	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}

	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

// 元の ResponseWriter が持つオプション機能を http.ResponseController から使えるようにする。
func (w *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (app *App) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		username := app.sessionUser(r.Context()).Username
		loggedWriter := &loggingResponseWriter{ResponseWriter: w}

		next.ServeHTTP(loggedWriter, r)
		if currentUsername := app.sessionUser(r.Context()).Username; currentUsername != "" {
			// ログインやプロキシ経由のユーザー切り替え後は、新しいユーザー名を記録する。
			username = currentUsername
		}

		status := loggedWriter.status
		if status == 0 {
			status = http.StatusOK
		}

		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		} else if status >= http.StatusBadRequest {
			level = slog.LevelWarn
		}

		app.logger.Log(r.Context(), level, "request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"bytes", loggedWriter.bytes,
			"duration", time.Since(started),
			"username", username,
			"remote", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
	})
}

// Loginを強制
func (app *App) requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if app.sessionUser(r.Context()).UserDN == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next(w, r)
	}
}
