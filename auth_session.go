package main

import "context"

const (
	sessionUsernameKey = "username"
	sessionUserDNKey   = "userDN"
)

type SessionUser struct {
	Username string
	UserDN   string
}

func (app *App) sessionUser(ctx context.Context) SessionUser {
	return SessionUser{
		Username: app.sessionManager.GetString(ctx, sessionUsernameKey),
		UserDN:   app.sessionManager.GetString(ctx, sessionUserDNKey),
	}
}

func (app *App) establishSession(ctx context.Context, user SessionUser) error {
	if err := app.sessionManager.RenewToken(ctx); err != nil {
		return err
	}
	if err := app.sessionManager.Clear(ctx); err != nil {
		return err
	}

	app.sessionManager.Put(ctx, sessionUsernameKey, user.Username)
	app.sessionManager.Put(ctx, sessionUserDNKey, user.UserDN)

	return nil
}

func (app *App) destroySession(ctx context.Context) error {
	return app.sessionManager.Destroy(ctx)
}
