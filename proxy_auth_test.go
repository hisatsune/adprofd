package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-ldap/ldap/v3"
)

func newProxyAuthTestApp(
	sessionManager *scs.SessionManager,
	token string,
	resolver func(string) (string, error),
) *App {
	return &App{
		logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		sessionManager:         sessionManager,
		proxyAuth:              ProxyAuthConfig{Token: token},
		resolveProxyAuthUserDN: resolver,
	}
}

func serveProxyAuthRequest(
	t *testing.T,
	app *App,
	cookie *http.Cookie,
	headers http.Header,
	next http.Handler,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	recorder := httptest.NewRecorder()
	handler := app.sessionManager.LoadAndSave(app.authenticateProxyAuth(next))
	handler.ServeHTTP(recorder, req)
	return recorder
}

func proxyAuthHeaders(username string, token string) http.Header {
	return http.Header{
		proxyAuthUserHeader:  []string{username},
		proxyAuthTokenHeader: []string{token},
	}
}

func TestAuthenticateProxyAuthSessionLifecycle(t *testing.T) {
	sessionManager := scs.New()
	sessionManager.Cookie.Name = "ADPROFDPROXYAUTHTEST"

	var resolvedUsers []string
	resolver := func(username string) (string, error) {
		resolvedUsers = append(resolvedUsers, username)
		switch username {
		case "alice":
			return "CN=Alice,OU=Users,DC=example,DC=test", nil
		case "bob":
			return "CN=Bob,OU=Users,DC=example,DC=test", nil
		default:
			return "", errors.New("user not found")
		}
	}

	app := newProxyAuthTestApp(sessionManager, "shared-token", resolver)

	firstResponse := serveProxyAuthRequest(
		t,
		app,
		nil,
		proxyAuthHeaders("alice", "shared-token"),
		http.HandlerFunc(app.getLogin),
	)
	if firstResponse.Code != http.StatusSeeOther {
		t.Fatalf("first response status = %d, want %d", firstResponse.Code, http.StatusSeeOther)
	}
	if location := firstResponse.Header().Get("Location"); location != "/profile" {
		t.Fatalf("first response Location = %q, want /profile", location)
	}
	if len(resolvedUsers) != 1 || resolvedUsers[0] != "alice" {
		t.Fatalf("resolved users = %v, want [alice]", resolvedUsers)
	}

	firstCookie := responseSessionCookie(t, firstResponse, sessionManager.Cookie.Name)

	var sameUser SessionUser
	var sameRequestUsesProxyAuth bool
	serveProxyAuthRequest(
		t,
		app,
		firstCookie,
		proxyAuthHeaders("ALICE", "shared-token"),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sameUser = app.sessionUser(r.Context())
			sameRequestUsesProxyAuth = requestUsesProxyAuth(r.Context())
		}),
	)
	if sameUser.Username != "alice" {
		t.Fatalf("same-user session username = %q, want alice", sameUser.Username)
	}
	if !sameRequestUsesProxyAuth {
		t.Fatal("requestUsesProxyAuth() = false, want true")
	}
	if len(resolvedUsers) != 1 {
		t.Fatalf("same user caused another LDAP lookup: %v", resolvedUsers)
	}

	secondResponse := serveProxyAuthRequest(
		t,
		app,
		firstCookie,
		proxyAuthHeaders("bob", "shared-token"),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	)
	secondCookie := responseSessionCookie(t, secondResponse, sessionManager.Cookie.Name)
	if secondCookie.Value == firstCookie.Value {
		t.Fatal("session token was not renewed when proxy user changed")
	}
	if len(resolvedUsers) != 2 || resolvedUsers[1] != "bob" {
		t.Fatalf("resolved users after switch = %v, want [alice bob]", resolvedUsers)
	}

	var switchedUser SessionUser
	runSessionRequest(t, sessionManager, secondCookie, func(ctx context.Context) {
		switchedUser = app.sessionUser(ctx)
	})
	wantSwitchedUser := SessionUser{
		Username: "bob",
		UserDN:   "CN=Bob,OU=Users,DC=example,DC=test",
	}
	if switchedUser != wantSwitchedUser {
		t.Fatalf("switched session user = %#v, want %#v", switchedUser, wantSwitchedUser)
	}
}

func TestAuthenticateProxyAuthRejectsInvalidCredentialsWithoutFallback(t *testing.T) {
	sessionManager := scs.New()
	sessionManager.Cookie.Name = "ADPROFDPROXYAUTHINVALIDTEST"
	app := newProxyAuthTestApp(sessionManager, "shared-token", func(username string) (string, error) {
		return "CN=Alice,OU=Users,DC=example,DC=test", nil
	})

	var establishErr error
	normalSessionResponse := runSessionRequest(t, sessionManager, nil, func(ctx context.Context) {
		establishErr = app.establishSession(ctx, SessionUser{
			Username: "form-user",
			UserDN:   "CN=Form User,OU=Users,DC=example,DC=test",
		})
	})
	if establishErr != nil {
		t.Fatalf("establishSession() error = %v", establishErr)
	}
	normalCookie := responseSessionCookie(t, normalSessionResponse, sessionManager.Cookie.Name)

	downstreamCalled := false
	response := serveProxyAuthRequest(
		t,
		app,
		normalCookie,
		proxyAuthHeaders("alice", "wrong-token"),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			downstreamCalled = true
		}),
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if downstreamCalled {
		t.Fatal("invalid proxy credentials fell back to the existing session")
	}

	app.resolveProxyAuthUserDN = func(username string) (string, error) {
		return "", errors.New("user not found")
	}
	downstreamCalled = false
	response = serveProxyAuthRequest(
		t,
		app,
		normalCookie,
		proxyAuthHeaders("unknown-user", "shared-token"),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			downstreamCalled = true
		}),
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if downstreamCalled {
		t.Fatal("failed proxy user lookup fell back to the existing session")
	}
}

func TestAuthenticateProxyAuthHeaderCombinations(t *testing.T) {
	tests := []struct {
		name            string
		configuredToken string
		headers         http.Header
		wantStatus      int
		wantDownstream  bool
		wantProxyAuth   bool
	}{
		{
			name:            "no proxy headers uses normal path",
			configuredToken: "shared-token",
			headers:         http.Header{},
			wantStatus:      http.StatusOK,
			wantDownstream:  true,
		},
		{
			name:            "user header only",
			configuredToken: "shared-token",
			headers: http.Header{
				proxyAuthUserHeader: []string{"alice"},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:            "token header only",
			configuredToken: "shared-token",
			headers: http.Header{
				proxyAuthTokenHeader: []string{"shared-token"},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:            "duplicate user header",
			configuredToken: "shared-token",
			headers: http.Header{
				proxyAuthUserHeader:  []string{"alice", "bob"},
				proxyAuthTokenHeader: []string{"shared-token"},
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:            "token configured as empty disables proxy authentication",
			configuredToken: "",
			headers:         proxyAuthHeaders("alice", "shared-token"),
			wantStatus:      http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionManager := scs.New()
			app := newProxyAuthTestApp(sessionManager, tt.configuredToken, func(username string) (string, error) {
				return "CN=Alice,OU=Users,DC=example,DC=test", nil
			})

			downstreamCalled := false
			usesProxyAuth := false
			response := serveProxyAuthRequest(
				t,
				app,
				nil,
				tt.headers,
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					downstreamCalled = true
					usesProxyAuth = requestUsesProxyAuth(r.Context())
				}),
			)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, tt.wantStatus)
			}
			if downstreamCalled != tt.wantDownstream {
				t.Fatalf("downstream called = %t, want %t", downstreamCalled, tt.wantDownstream)
			}
			if usesProxyAuth != tt.wantProxyAuth {
				t.Fatalf("requestUsesProxyAuth() = %t, want %t", usesProxyAuth, tt.wantProxyAuth)
			}
		})
	}
}

func TestProfileTemplateHidesLogoutForProxyAuth(t *testing.T) {
	tmpl, err := newTemplates()
	if err != nil {
		t.Fatalf("newTemplates() error = %v", err)
	}

	tests := []struct {
		name               string
		proxyAuthenticated bool
		wantLogout         bool
	}{
		{name: "normal session", wantLogout: true},
		{name: "proxy authentication", proxyAuthenticated: true, wantLogout: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			err := tmpl.ExecuteTemplate(&output, "profile.html", ProfilePageData{
				Title:              "プロフィール編集",
				ProxyAuthenticated: tt.proxyAuthenticated,
				FieldErrors:        map[string]string{},
				FieldOK:            map[string]bool{},
			})
			if err != nil {
				t.Fatalf("ExecuteTemplate() error = %v", err)
			}

			hasLogout := strings.Contains(output.String(), `action="/logout"`)
			if hasLogout != tt.wantLogout {
				t.Fatalf("logout form present = %t, want %t", hasLogout, tt.wantLogout)
			}
		})
	}
}

func TestInvalidSessionDNClassification(t *testing.T) {
	noSuchObject := ldap.NewError(ldap.LDAPResultNoSuchObject, errors.New("missing"))
	if !isLDAPNoSuchObject(fmt.Errorf("read entry: %w", noSuchObject)) {
		t.Fatal("LDAP no-such-object error was not recognized")
	}
	if isLDAPNoSuchObject(ldap.NewError(ldap.LDAPResultUnavailable, errors.New("unavailable"))) {
		t.Fatal("temporary LDAP failure was classified as a missing object")
	}
}

func TestRedirectAfterInvalidUserDN(t *testing.T) {
	tests := []struct {
		name       string
		proxyAuth  bool
		wantTarget string
	}{
		{name: "normal session", wantTarget: "/login?session=invalid"},
		{name: "proxy authentication", proxyAuth: true, wantTarget: "/profile?session=refreshed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionManager := scs.New()
			sessionManager.Cookie.Name = "ADPROFDINVALIDDNTEST"
			app := newProxyAuthTestApp(sessionManager, "shared-token", nil)

			var establishErr error
			loginResponse := runSessionRequest(t, sessionManager, nil, func(ctx context.Context) {
				establishErr = app.establishSession(ctx, SessionUser{
					Username: "alice",
					UserDN:   "CN=Old Alice,OU=Users,DC=example,DC=test",
				})
			})
			if establishErr != nil {
				t.Fatalf("establishSession() error = %v", establishErr)
			}
			cookie := responseSessionCookie(t, loginResponse, sessionManager.Cookie.Name)

			req := httptest.NewRequest(http.MethodGet, "/profile", nil)
			req.AddCookie(cookie)
			recorder := httptest.NewRecorder()
			handler := sessionManager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.proxyAuth {
					ctx := context.WithValue(r.Context(), proxyAuthContextKey{}, true)
					r = r.WithContext(ctx)
				}
				app.redirectAfterInvalidUserDN(w, r)
			}))
			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusSeeOther)
			}
			if target := recorder.Header().Get("Location"); target != tt.wantTarget {
				t.Fatalf("Location = %q, want %q", target, tt.wantTarget)
			}
			destroyCookie := responseSessionCookie(t, recorder, sessionManager.Cookie.Name)
			if destroyCookie.MaxAge >= 0 {
				t.Fatalf("destroy cookie MaxAge = %d, want negative", destroyCookie.MaxAge)
			}
		})
	}
}
