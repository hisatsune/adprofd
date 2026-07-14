package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
)

func runSessionRequest(
	t *testing.T,
	sessionManager *scs.SessionManager,
	cookie *http.Cookie,
	fn func(context.Context),
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler := sessionManager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fn(r.Context())
	}))
	handler.ServeHTTP(recorder, req)

	return recorder
}

func responseSessionCookie(t *testing.T, recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()

	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}

	t.Fatalf("response cookie %q not found", name)
	return nil
}

func TestAuthSessionLifecycle(t *testing.T) {
	sessionManager := scs.New()
	sessionManager.Cookie.Name = "ADPROFDTESTSESSION"
	app := &App{sessionManager: sessionManager}

	initialUser := SessionUser{
		Username: "alice",
		UserDN:   "CN=Alice,OU=Users,DC=example,DC=test",
	}

	var establishErr error
	var establishedUser SessionUser
	firstResponse := runSessionRequest(t, sessionManager, nil, func(ctx context.Context) {
		establishErr = app.establishSession(ctx, initialUser)
		sessionManager.Put(ctx, "cachedPermission", "admin")
		establishedUser = app.sessionUser(ctx)
	})
	if establishErr != nil {
		t.Fatalf("establishSession() error = %v", establishErr)
	}
	if establishedUser != initialUser {
		t.Fatalf("sessionUser() = %#v, want %#v", establishedUser, initialUser)
	}

	firstCookie := responseSessionCookie(t, firstResponse, sessionManager.Cookie.Name)

	var persistedUser SessionUser
	runSessionRequest(t, sessionManager, firstCookie, func(ctx context.Context) {
		persistedUser = app.sessionUser(ctx)
	})
	if persistedUser != initialUser {
		t.Fatalf("persisted sessionUser() = %#v, want %#v", persistedUser, initialUser)
	}

	switchedUser := SessionUser{
		Username: "bob",
		UserDN:   "CN=Bob,OU=Users,DC=example,DC=test",
	}

	var switchErr error
	secondResponse := runSessionRequest(t, sessionManager, firstCookie, func(ctx context.Context) {
		switchErr = app.establishSession(ctx, switchedUser)
	})
	if switchErr != nil {
		t.Fatalf("establishSession() on user switch error = %v", switchErr)
	}

	secondCookie := responseSessionCookie(t, secondResponse, sessionManager.Cookie.Name)
	if secondCookie.Value == firstCookie.Value {
		t.Fatal("session token was not renewed on user switch")
	}

	var switchedSessionUser SessionUser
	var cachedPermission string
	runSessionRequest(t, sessionManager, secondCookie, func(ctx context.Context) {
		switchedSessionUser = app.sessionUser(ctx)
		cachedPermission = sessionManager.GetString(ctx, "cachedPermission")
	})
	if switchedSessionUser != switchedUser {
		t.Fatalf("switched sessionUser() = %#v, want %#v", switchedSessionUser, switchedUser)
	}
	if cachedPermission != "" {
		t.Fatalf("cached permission survived user switch: %q", cachedPermission)
	}

	var destroyErr error
	var destroyedUser SessionUser
	destroyResponse := runSessionRequest(t, sessionManager, secondCookie, func(ctx context.Context) {
		destroyErr = app.destroySession(ctx)
		destroyedUser = app.sessionUser(ctx)
	})
	if destroyErr != nil {
		t.Fatalf("destroySession() error = %v", destroyErr)
	}
	if destroyedUser != (SessionUser{}) {
		t.Fatalf("sessionUser() after destroy = %#v, want empty", destroyedUser)
	}

	destroyCookie := responseSessionCookie(t, destroyResponse, sessionManager.Cookie.Name)
	if destroyCookie.MaxAge >= 0 {
		t.Fatalf("destroy cookie MaxAge = %d, want negative", destroyCookie.MaxAge)
	}
}
