package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/aluko123/go-network-proxy/pkg/auth"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
)

type authContextKey string

const APIKeyInfoKey authContextKey = "api_key_info"

func WithAPIKeyAuth(ks *auth.KeyStore) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				metrics.AuthFailuresTotal.WithLabelValues("missing_token").Inc()
				http.Error(w, "Authorization header required", http.StatusUnauthorized)
				return
			}

			keyInfo, valid := ks.Validate(token)
			if !valid {
				metrics.AuthFailuresTotal.WithLabelValues("invalid_token").Inc()
				http.Error(w, "Invalid API key", http.StatusUnauthorized)
				return
			}

			metrics.AuthSuccessTotal.Inc()

			ctx := context.WithValue(r.Context(), APIKeyInfoKey, keyInfo)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}

	return strings.TrimSpace(parts[1])
}

func GetAPIKeyInfo(ctx context.Context) (auth.KeyInfo, bool) {
	info, ok := ctx.Value(APIKeyInfoKey).(auth.KeyInfo)
	return info, ok
}
