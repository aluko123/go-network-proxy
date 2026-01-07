package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aluko123/go-network-proxy/pkg/auth"
)

func newTestKeyStore() *auth.KeyStore {
	ks := auth.NewKeyStore()
	// Access internal map for testing - in production use LoadFromFile
	// For this test we'll use the Validate method behavior
	return ks
}

func TestWithAPIKeyAuth_NoHeader(t *testing.T) {
	ks := auth.NewKeyStore()
	
	handler := WithAPIKeyAuth(ks)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/v1/inference", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestWithAPIKeyAuth_InvalidToken(t *testing.T) {
	ks := auth.NewKeyStore()
	
	handler := WithAPIKeyAuth(ks)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/v1/inference", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestWithAPIKeyAuth_MalformedHeader(t *testing.T) {
	ks := auth.NewKeyStore()
	
	handler := WithAPIKeyAuth(ks)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testCases := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "sk-valid-key"},
		{"basic auth", "Basic dXNlcjpwYXNz"},
		{"empty bearer", "Bearer "},
		{"bearer only", "Bearer"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/inference", nil)
			req.Header.Set("Authorization", tc.header)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", rec.Code)
			}
		})
	}
}

func TestWithAPIKeyAuth_ValidToken(t *testing.T) {
	ks := auth.NewKeyStore()
	if err := ks.LoadFromFile("../../configs/apikeys.json"); err != nil {
		t.Skipf("skipping test, could not load apikeys.json: %v", err)
	}

	handlerCalled := false
	handler := WithAPIKeyAuth(ks)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/v1/inference", nil)
	req.Header.Set("Authorization", "Bearer sk-dev-test-key-12345")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !handlerCalled {
		t.Error("next handler was not called")
	}
}

func TestWithAPIKeyAuth_CaseInsensitiveBearer(t *testing.T) {
	ks := auth.NewKeyStore()
	if err := ks.LoadFromFile("../../configs/apikeys.json"); err != nil {
		t.Skipf("skipping test, could not load apikeys.json: %v", err)
	}

	handler := WithAPIKeyAuth(ks)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testCases := []string{"Bearer", "bearer", "BEARER", "BeArEr"}

	for _, prefix := range testCases {
		t.Run(prefix, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/v1/inference", nil)
			req.Header.Set("Authorization", prefix+" sk-dev-test-key-12345")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("expected 200 for prefix '%s', got %d", prefix, rec.Code)
			}
		})
	}
}

func TestWithAPIKeyAuth_ContextContainsKeyInfo(t *testing.T) {
	ks := auth.NewKeyStore()
	if err := ks.LoadFromFile("../../configs/apikeys.json"); err != nil {
		t.Skipf("skipping test, could not load apikeys.json: %v", err)
	}

	var capturedKeyInfo auth.KeyInfo
	var keyInfoFound bool

	handler := WithAPIKeyAuth(ks)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedKeyInfo, keyInfoFound = GetAPIKeyInfo(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/v1/inference", nil)
	req.Header.Set("Authorization", "Bearer sk-dev-test-key-12345")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !keyInfoFound {
		t.Error("KeyInfo not found in context")
	}
	if capturedKeyInfo.Name != "dev-local" {
		t.Errorf("expected name 'dev-local', got '%s'", capturedKeyInfo.Name)
	}
}

func TestExtractBearerToken(t *testing.T) {
	testCases := []struct {
		name     string
		header   string
		expected string
	}{
		{"valid bearer", "Bearer sk-test-123", "sk-test-123"},
		{"lowercase bearer", "bearer sk-test-123", "sk-test-123"},
		{"no header", "", ""},
		{"no bearer prefix", "sk-test-123", ""},
		{"basic auth", "Basic dXNlcjpwYXNz", ""},
		{"empty bearer", "Bearer ", ""},
		{"bearer with spaces", "Bearer  sk-test-123 ", "sk-test-123"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}

			token := extractBearerToken(req)
			if token != tc.expected {
				t.Errorf("expected '%s', got '%s'", tc.expected, token)
			}
		})
	}
}
