package main

import (
	"encoding/base64"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecodePhotoDataURL(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		want := []byte("jpeg payload")
		encoded := base64.StdEncoding.EncodeToString(want)

		got, err := decodePhotoDataURL("data:image/jpeg;base64," + encoded)
		if err != nil {
			t.Fatalf("decodePhotoDataURL() error = %v", err)
		}
		if string(got) != string(want) {
			t.Fatalf("decodePhotoDataURL() = %q, want %q", got, want)
		}
	})

	t.Run("invalid data URL", func(t *testing.T) {
		if _, err := decodePhotoDataURL("data:image/png;base64,AAAA"); err == nil {
			t.Fatal("decodePhotoDataURL() error = nil, want error")
		}
	})

	t.Run("invalid base64", func(t *testing.T) {
		if _, err := decodePhotoDataURL("data:image/jpeg;base64,not-base64"); err == nil {
			t.Fatal("decodePhotoDataURL() error = nil, want error")
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		if _, err := decodePhotoDataURL("data:image/jpeg;base64,"); err == nil {
			t.Fatal("decodePhotoDataURL() error = nil, want error")
		}
	})

	t.Run("too many bytes", func(t *testing.T) {
		data := make([]byte, maxThumbnailBytes+1)
		encoded := base64.StdEncoding.EncodeToString(data)

		_, err := decodePhotoDataURL("data:image/jpeg;base64," + encoded)
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("decodePhotoDataURL() error = %v, want too large error", err)
		}
	})
}

func TestProfileSaveStatus(t *testing.T) {
	tests := []struct {
		name         string
		result       SaveProfileResult
		reloadFailed bool
		want         int
	}{
		{
			name:   "success",
			result: SaveProfileResult{},
			want:   http.StatusOK,
		},
		{
			name: "validation error",
			result: SaveProfileResult{
				FieldErrors:      map[string]string{"photo": "invalid"},
				ValidationErrors: map[string]bool{"photo": true},
			},
			want: http.StatusBadRequest,
		},
		{
			name: "LDAP error",
			result: SaveProfileResult{
				FieldErrors:  map[string]string{"photo": "failed"},
				ServerErrors: map[string]bool{"photo": true},
			},
			want: http.StatusInternalServerError,
		},
		{
			name: "server error takes precedence over validation error",
			result: SaveProfileResult{
				FieldErrors: map[string]string{
					"photo":    "failed",
					"password": "invalid",
				},
				ValidationErrors: map[string]bool{"password": true},
				ServerErrors:     map[string]bool{"photo": true},
			},
			want: http.StatusInternalServerError,
		},
		{
			name:         "reload error",
			result:       SaveProfileResult{},
			reloadFailed: true,
			want:         http.StatusInternalServerError,
		},
		{
			name: "unclassified field error fails safe",
			result: SaveProfileResult{
				FieldErrors: map[string]string{"unknown": "failed"},
			},
			want: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileSaveStatus(tt.result, tt.reloadFailed); got != tt.want {
				t.Fatalf("profileSaveStatus() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSortedFieldNames(t *testing.T) {
	got := sortedFieldNames(map[string]bool{
		"street": true,
		"photo":  true,
		"city":   true,
	})
	want := []string{"city", "photo", "street"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedFieldNames() = %v, want %v", got, want)
	}
}

func TestValidatePasswordChange(t *testing.T) {
	tests := []struct {
		name  string
		input ProfileInput
		want  map[string]string
	}{
		{
			name:  "not requested",
			input: ProfileInput{},
			want:  map[string]string{},
		},
		{
			name: "valid",
			input: ProfileInput{
				CurrentPassword:    "current",
				NewPassword:        "new-password",
				NewPasswordConfirm: "new-password",
			},
			want: map[string]string{},
		},
		{
			name: "all fields are required when requested",
			input: ProfileInput{
				NewPassword: "new-password",
			},
			want: map[string]string{
				"current_password":     "現在のパスワードを入力してください。",
				"new_password_confirm": "確認用パスワードを入力してください。",
			},
		},
		{
			name: "confirmation mismatch",
			input: ProfileInput{
				CurrentPassword:    "current",
				NewPassword:        "new-password",
				NewPasswordConfirm: "different",
			},
			want: map[string]string{
				"new_password_confirm": "新しいパスワードが一致しません。",
			},
		},
		{
			name: "new password must differ",
			input: ProfileInput{
				CurrentPassword:    "same-password",
				NewPassword:        "same-password",
				NewPasswordConfirm: "same-password",
			},
			want: map[string]string{
				"new_password": "現在とは異なるパスワードを入力してください。",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validatePasswordChange(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("validatePasswordChange() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPasswordTooYoungMessage(t *testing.T) {
	jst := time.FixedZone("JST", 9*60*60)
	allowedAt := time.Date(2026, 7, 14, 5, 20, 54, 0, jst)
	want := "ADの最小変更間隔により、パスワードは2026年7月14日 05:20:54 JST以降に変更できます。"

	if got := passwordTooYoungMessage(allowedAt); got != want {
		t.Fatalf("passwordTooYoungMessage() = %q, want %q", got, want)
	}
}
