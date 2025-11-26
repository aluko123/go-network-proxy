package middleware

import (
	"context"
	"net/http"

	"github.com/aluko123/go-network-proxy/pkg/logger"
	"github.com/google/uuid"
)

func WithRequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			// check for existing request header
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = uuid.New().String()
			}

			//store in context
			ctx := context.WithValue(r.Context(), logger.RequestIDKey, id)

			// set response header
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
