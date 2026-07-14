package main

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
)

func TestEncodeUnicodePassword(t *testing.T) {
	got := encodeUnicodePassword("P@ss")
	want := []byte{
		0x22, 0x00,
		0x50, 0x00,
		0x40, 0x00,
		0x73, 0x00,
		0x73, 0x00,
		0x22, 0x00,
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("encodeUnicodePassword() = %x, want %x", got, want)
	}
}

func TestValidateLDAPSURL(t *testing.T) {
	if err := validateLDAPSURL("ldaps://dc.example.com:636"); err != nil {
		t.Fatalf("validateLDAPSURL() error = %v", err)
	}
	if err := validateLDAPSURL("ldap://dc.example.com:389"); err == nil {
		t.Fatal("validateLDAPSURL() error = nil, want error")
	}
}

func TestNewOwnPasswordModifyRequest(t *testing.T) {
	req := newOwnPasswordModifyRequest("CN=Test User,DC=example,DC=com", "old", "new")

	if req.DN != "CN=Test User,DC=example,DC=com" {
		t.Fatalf("DN = %q", req.DN)
	}
	if len(req.Changes) != 2 {
		t.Fatalf("len(Changes) = %d, want 2", len(req.Changes))
	}

	oldChange := req.Changes[0]
	if oldChange.Operation != ldap.DeleteAttribute || oldChange.Modification.Type != "unicodePwd" {
		t.Fatalf("first change = %#v, want unicodePwd delete", oldChange)
	}
	if len(oldChange.Modification.Vals) != 1 ||
		!bytes.Equal([]byte(oldChange.Modification.Vals[0]), encodeUnicodePassword("old")) {
		t.Fatalf("first change does not contain the encoded old password")
	}

	newChange := req.Changes[1]
	if newChange.Operation != ldap.AddAttribute || newChange.Modification.Type != "unicodePwd" {
		t.Fatalf("second change = %#v, want unicodePwd add", newChange)
	}
	if len(newChange.Modification.Vals) != 1 ||
		!bytes.Equal([]byte(newChange.Modification.Vals[0]), encodeUnicodePassword("new")) {
		t.Fatalf("second change does not contain the encoded new password")
	}
}

func TestIsPasswordPolicyError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "constraint violation",
			err:  ldap.NewError(ldap.LDAPResultConstraintViolation, errors.New("password restriction")),
			want: true,
		},
		{
			name: "unwilling to perform",
			err:  ldap.NewError(ldap.LDAPResultUnwillingToPerform, errors.New("password restriction")),
			want: true,
		},
		{
			name: "insufficient access rights",
			err:  ldap.NewError(ldap.LDAPResultInsufficientAccessRights, errors.New("denied")),
			want: false,
		},
		{
			name: "network error",
			err:  errors.New("network error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPasswordPolicyError(tt.err); got != tt.want {
				t.Fatalf("isPasswordPolicyError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsPasswordTooYoungError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "constraint violation with diagnostic",
			err: ldap.NewError(
				ldap.LDAPResultConstraintViolation,
				errors.New("0000052D: check_password_restrictions: password is too young to change!"),
			),
			want: true,
		},
		{
			name: "diagnostic is case insensitive",
			err: ldap.NewError(
				ldap.LDAPResultUnwillingToPerform,
				errors.New("Password Is Too Young To Change"),
			),
			want: true,
		},
		{
			name: "52D without specific diagnostic remains generic policy error",
			err: ldap.NewError(
				ldap.LDAPResultConstraintViolation,
				errors.New("0000052D: password does not meet complexity requirements"),
			),
			want: false,
		},
		{
			name: "same text in a non LDAP error is not classified",
			err:  errors.New("password is too young to change"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isPasswordTooYoungError(tt.err); got != tt.want {
				t.Fatalf("isPasswordTooYoungError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseADFileTime(t *testing.T) {
	got, err := parseADFileTime("116444736000000000")
	if err != nil {
		t.Fatalf("parseADFileTime() error = %v", err)
	}
	if want := time.Unix(0, 0).UTC(); !got.Equal(want) {
		t.Fatalf("parseADFileTime() = %s, want %s", got, want)
	}

	for _, value := range []string{"", "invalid", "0", "-1"} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseADFileTime(value); err == nil {
				t.Fatalf("parseADFileTime(%q) error = nil, want error", value)
			}
		})
	}
}

func TestParseADInterval(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "one day", value: "-864000000000", want: 24 * time.Hour},
		{name: "zero", value: "0", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseADInterval(tt.value)
			if err != nil {
				t.Fatalf("parseADInterval() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseADInterval() = %s, want %s", got, tt.want)
			}
		})
	}

	for _, value := range []string{"", "invalid", "1", "-9223372036854775808"} {
		t.Run(value, func(t *testing.T) {
			if _, err := parseADInterval(value); err == nil {
				t.Fatalf("parseADInterval(%q) error = nil, want error", value)
			}
		})
	}
}
