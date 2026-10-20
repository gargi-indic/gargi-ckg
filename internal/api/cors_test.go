package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gargi-indic/gargi-flow/internal/api"
)

func TestCORSMiddleware(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	tests := []struct {
		name               string
		allowedOrigins     []string
		method             string
		origin             string
		expectedStatus     int
		expectedCORSHeader string
		expectedBody       string
	}{
		{
			name:               "allowed origin GET",
			allowedOrigins:     []string{"http://localhost:5173"},
			method:             http.MethodGet,
			origin:             "http://localhost:5173",
			expectedStatus:     http.StatusOK,
			expectedCORSHeader: "http://localhost:5173",
			expectedBody:       "OK",
		},
		{
			name:               "allowed origin POST",
			allowedOrigins:     []string{"http://localhost:5173"},
			method:             http.MethodPost,
			origin:             "http://localhost:5173",
			expectedStatus:     http.StatusOK,
			expectedCORSHeader: "http://localhost:5173",
			expectedBody:       "OK",
		},
		{
			name:               "allowed origin OPTIONS (preflight)",
			allowedOrigins:     []string{"http://localhost:5173"},
			method:             http.MethodOptions,
			origin:             "http://localhost:5173",
			expectedStatus:     http.StatusNoContent,
			expectedCORSHeader: "http://localhost:5173",
			expectedBody:       "",
		},
		{
			name:               "wildcard origin",
			allowedOrigins:     []string{"*"},
			method:             http.MethodGet,
			origin:             "https://example.com",
			expectedStatus:     http.StatusOK,
			expectedCORSHeader: "https://example.com",
			expectedBody:       "OK",
		},
		{
			name:               "rejected origin",
			allowedOrigins:     []string{"http://localhost:5173"},
			method:             http.MethodGet,
			origin:             "https://malicious.com",
			expectedStatus:     http.StatusOK, // The request goes through, just without CORS headers
			expectedCORSHeader: "",
			expectedBody:       "OK",
		},
		{
			name:               "rejected origin OPTIONS",
			allowedOrigins:     []string{"http://localhost:5173"},
			method:             http.MethodOptions,
			origin:             "https://malicious.com",
			expectedStatus:     http.StatusOK, // Passed to next handler since origin not allowed
			expectedCORSHeader: "",
			expectedBody:       "OK",
		},
		{
			name:               "no origin header",
			allowedOrigins:     []string{"http://localhost:5173"},
			method:             http.MethodGet,
			origin:             "",
			expectedStatus:     http.StatusOK,
			expectedCORSHeader: "",
			expectedBody:       "OK",
		},
		{
			name:               "case insensitive match",
			allowedOrigins:     []string{"http://LOCALhost:5173"},
			method:             http.MethodGet,
			origin:             "http://localhost:5173",
			expectedStatus:     http.StatusOK,
			expectedCORSHeader: "http://localhost:5173",
			expectedBody:       "OK",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := api.CORSMiddleware(tt.allowedOrigins, nextHandler)
			req := httptest.NewRequest(tt.method, "/", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, rec.Code)
			}

			corsHeader := rec.Header().Get("Access-Control-Allow-Origin")
			if corsHeader != tt.expectedCORSHeader {
				t.Errorf("expected CORS header %q, got %q", tt.expectedCORSHeader, corsHeader)
			}

			if tt.expectedCORSHeader != "" {
				if rec.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
					t.Errorf("expected Access-Control-Allow-Methods 'GET, POST, OPTIONS', got %q", rec.Header().Get("Access-Control-Allow-Methods"))
				}
				if rec.Header().Get("Access-Control-Allow-Headers") != "Content-Type, Authorization" {
					t.Errorf("expected Access-Control-Allow-Headers 'Content-Type, Authorization', got %q", rec.Header().Get("Access-Control-Allow-Headers"))
				}
			}

			if rec.Body.String() != tt.expectedBody {
				t.Errorf("expected body %q, got %q", tt.expectedBody, rec.Body.String())
			}
		})
	}
}
