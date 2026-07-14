package main

import (
	"net/http"
	"strings"
)

type PageData struct {
	Title   string
	Error   string
	Success string
}

func (app *App) redirectRoot(w http.ResponseWriter, r *http.Request) {
	if app.sessionUser(r.Context()).UserDN != "" {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (app *App) getLogin(w http.ResponseWriter, r *http.Request) {
	if app.sessionUser(r.Context()).UserDN != "" {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}

	data := PageData{
		Title:   "AD Profile Login",
		Success: app.sessionManager.PopString(r.Context(), "login_success"),
	}
	if r.URL.Query().Get("session") == "invalid" {
		data.Error = "AD のユーザー情報が変更されたため、もう一度ログインしてください。"
	}

	app.render(w, http.StatusOK, "login", data)
}

func (app *App) postLogin(w http.ResponseWriter, r *http.Request) {
	if requestUsesProxyAuth(r.Context()) {
		http.Redirect(w, r, "/profile", http.StatusSeeOther)
		return
	}

	if err := r.ParseForm(); err != nil {
		app.render(w, http.StatusBadRequest, "login", PageData{
			Title: "AD Profile Login",
			Error: "フォームの読み取りに失敗しました。",
		})
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if username == "" || password == "" {
		app.render(w, http.StatusBadRequest, "login", PageData{
			Title: "AD Profile Login",
			Error: "ユーザー名とパスワードを入力してください。",
		})
		return
	}

	app.logger.Info("login attempt",
		"username", username,
		"remote", r.RemoteAddr,
	)

	// LDAP login
	userDN, err := app.ldap.authenticateLDAP(username, password)
	if err != nil {
		app.logger.Info("ldap authentication failed",
			"username", username,
			"remote", r.RemoteAddr,
			"error", err,
		)

		app.render(w, http.StatusUnauthorized, "login", PageData{
			Title: "AD Profile Login",
			Error: "ユーザー名またはパスワードが違います。",
		})
		return
	}

	if err := app.establishSession(r.Context(), SessionUser{
		Username: username,
		UserDN:   userDN,
	}); err != nil {
		app.logger.Error("establish session failed", "error", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	app.logger.Info("login succeeded",
		"username", username,
		"user_dn", userDN,
		"remote", r.RemoteAddr,
	)

	http.Redirect(w, r, "/profile", http.StatusSeeOther)
}

func (app *App) logout(w http.ResponseWriter, r *http.Request) {
	if err := app.destroySession(r.Context()); err != nil {
		app.logger.Error("destroy session failed", "error", err)
		http.Error(w, "session error", http.StatusInternalServerError)
		return
	}

	app.render(w, http.StatusOK, "logout", PageData{
		Title: "ログアウト",
	})
}
