package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoggingResponseWriter(t *testing.T) {
	t.Run("records explicit status and bytes", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		writer := &loggingResponseWriter{ResponseWriter: recorder}

		writer.WriteHeader(http.StatusBadRequest)
		if _, err := writer.Write([]byte("error")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		if writer.status != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", writer.status, http.StatusBadRequest)
		}
		if writer.bytes != len("error") {
			t.Fatalf("bytes = %d, want %d", writer.bytes, len("error"))
		}
	})

	t.Run("implicit status is OK", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		writer := &loggingResponseWriter{ResponseWriter: recorder}

		if _, err := writer.Write([]byte("ok")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		if writer.status != http.StatusOK {
			t.Fatalf("status = %d, want %d", writer.status, http.StatusOK)
		}
	})
}
