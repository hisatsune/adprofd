package main

import (
	"os"
	"testing"
)

func TestEnvStringDefault(t *testing.T) {
	const name = "ADPROFD_TEST_STRING_DEFAULT"

	tests := []struct {
		name  string
		value string
		set   bool
		want  string
	}{
		{name: "unset", want: "default-value"},
		{name: "empty", value: "", set: true, want: "default-value"},
		{name: "whitespace", value: "  ", set: true, want: "default-value"},
		{name: "configured", value: "/tmp/adprofd/session.sqlite3", set: true, want: "/tmp/adprofd/session.sqlite3"},
		{name: "trimmed", value: " /tmp/adprofd/session.sqlite3 ", set: true, want: "/tmp/adprofd/session.sqlite3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(name, "")
			if tt.set {
				t.Setenv(name, tt.value)
			} else if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}

			if got := envStringDefault(name, "default-value"); got != tt.want {
				t.Fatalf("envStringDefault() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnvBoolDefaultTrue(t *testing.T) {
	const name = "ADPROFD_TEST_BOOL_DEFAULT_TRUE"

	tests := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "unset", want: true},
		{name: "empty", value: "", set: true, want: true},
		{name: "true", value: "true", set: true, want: true},
		{name: "false", value: "false", set: true, want: false},
		{name: "false is case insensitive", value: " FALSE ", set: true, want: false},
		{name: "unknown value fails closed", value: "disabled", set: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(name, "")
			if tt.set {
				t.Setenv(name, tt.value)
			} else {
				if err := os.Unsetenv(name); err != nil {
					t.Fatal(err)
				}
			}

			if got := envBoolDefaultTrue(name); got != tt.want {
				t.Fatalf("envBoolDefaultTrue() = %t, want %t", got, tt.want)
			}
		})
	}
}
