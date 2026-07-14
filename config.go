package main

import (
	"os"
	"strings"
)

const defaultSessionDBPath = "/var/lib/adprofd/session.sqlite3"

type Config struct {
	ListenAddr string
	LDAP       LDAPConfig
	ProxyAuth  ProxyAuthConfig
	Session    SessionConfig
}

type LDAPConfig struct {
	URL          string
	BaseDN       string
	BindDN       string
	BindPassword string
	ServerName   string
}

type ProxyAuthConfig struct {
	Token string
}

type SessionConfig struct {
	DBPath       string
	CookieSecure bool
}

func envStringDefault(name, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return defaultValue
	}

	return value
}

func envBoolDefaultTrue(name string) bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(name)), "false")
}

func loadConfig() Config {
	return Config{
		ListenAddr: os.Getenv("ADPROFD_LISTEN_ADDR"),
		LDAP: LDAPConfig{
			URL:          os.Getenv("ADPROFD_LDAP_URL"),
			BaseDN:       os.Getenv("ADPROFD_LDAP_BASE_DN"),
			BindDN:       os.Getenv("ADPROFD_LDAP_BIND_DN"),
			BindPassword: os.Getenv("ADPROFD_LDAP_BIND_PASSWORD"),
			ServerName:   os.Getenv("ADPROFD_LDAP_TLS_SERVER_NAME"),
		},
		ProxyAuth: ProxyAuthConfig{
			Token: os.Getenv("ADPROFD_PROXYAUTH_TOKEN"),
		},
		Session: SessionConfig{
			DBPath:       envStringDefault("ADPROFD_SESSION_DB_PATH", defaultSessionDBPath),
			CookieSecure: envBoolDefaultTrue("ADPROFD_SESSION_COOKIE_SECURE"),
		},
	}
}
