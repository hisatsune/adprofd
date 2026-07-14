package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

const (
	proxyAuthUserHeader  = "X-PROXYAUTH-USER"
	proxyAuthTokenHeader = "X-PROXYAUTH-TOKEN"
)

var errInvalidProxyAuthCredentials = errors.New("invalid proxy authentication credentials")

type proxyAuthContextKey struct{}

func requestUsesProxyAuth(ctx context.Context) bool {
	authenticated, _ := ctx.Value(proxyAuthContextKey{}).(bool)
	return authenticated
}

func tokensMatch(actual string, expected string) bool {
	actualHash := sha256.Sum256([]byte(actual))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(actualHash[:], expectedHash[:]) == 1
}

func (app *App) proxyAuthUsername(r *http.Request) (string, bool, error) {
	users := r.Header.Values(proxyAuthUserHeader)
	tokens := r.Header.Values(proxyAuthTokenHeader)

	if len(users) == 0 && len(tokens) == 0 {
		return "", false, nil
	}

	if len(users) != 1 || len(tokens) != 1 {
		return "", true, errInvalidProxyAuthCredentials
	}

	username := strings.TrimSpace(users[0])
	if username == "" || app.proxyAuth.Token == "" || !tokensMatch(tokens[0], app.proxyAuth.Token) {
		return "", true, errInvalidProxyAuthCredentials
	}

	return username, true, nil
}

func sameUsername(a string, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func (app *App) authenticateProxyAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, supplied, err := app.proxyAuthUsername(r)
		if err != nil {
			app.logger.Warn("proxy authentication rejected",
				"remote", r.RemoteAddr,
				"user_header_present", len(r.Header.Values(proxyAuthUserHeader)) > 0,
				"token_header_present", len(r.Header.Values(proxyAuthTokenHeader)) > 0,
			)
			http.Error(w, "proxy authentication failed", http.StatusUnauthorized)
			return
		}
		if !supplied {
			next.ServeHTTP(w, r)
			return
		}

		user := app.sessionUser(r.Context())
		if user.UserDN == "" || !sameUsername(user.Username, username) {
			if app.resolveProxyAuthUserDN == nil {
				app.logger.Error("proxy authentication user resolver is not configured")
				http.Error(w, "proxy authentication error", http.StatusInternalServerError)
				return
			}

			userDN, err := app.resolveProxyAuthUserDN(username)
			if err != nil {
				app.logger.Info("proxy authentication user lookup failed",
					"username", username,
					"remote", r.RemoteAddr,
					"error", err,
				)
				http.Error(w, "proxy authentication failed", http.StatusUnauthorized)
				return
			}

			user = SessionUser{Username: username, UserDN: userDN}
			if err := app.establishSession(r.Context(), user); err != nil {
				app.logger.Error("establish proxy authentication session failed", "error", err)
				http.Error(w, "session error", http.StatusInternalServerError)
				return
			}

			app.logger.Info("proxy authentication login succeeded",
				"username", username,
				"user_dn", userDN,
				"remote", r.RemoteAddr,
			)
		}

		ctx := context.WithValue(r.Context(), proxyAuthContextKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
