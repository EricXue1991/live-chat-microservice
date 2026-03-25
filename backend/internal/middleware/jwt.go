package middleware

import (
	"context"
	"net/http"
	"strings"

	"livechat/internal/config"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const UsernameKey contextKey = "username"

// JWTAuth validates the Bearer token in the Authorization header.
// On success, it injects the username into the request context.
//
// Why JWT instead of sessions:
//   - Stateless: any replica can verify without a shared session store
//   - Critical for horizontal scaling: adding replicas needs no session affinity
func JWTAuth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization header"}`, http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				http.Error(w, `{"error":"invalid authorization format"}`, http.StatusUnauthorized)
				return
			}

			token, err := jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
				// Verify signing method is HMAC to prevent algorithm-confusion attacks
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return []byte(cfg.JWTSecret), nil
			})

			if err != nil || !token.Valid {
				http.Error(w, `{"error":"invalid or expired token"}`, http.StatusUnauthorized)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				http.Error(w, `{"error":"invalid token claims"}`, http.StatusUnauthorized)
				return
			}

			username, _ := claims["username"].(string)
			if username == "" {
				http.Error(w, `{"error":"missing username in token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UsernameKey, username)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetUsername extracts the authenticated username from the request context.
func GetUsername(r *http.Request) string {
	if u, ok := r.Context().Value(UsernameKey).(string); ok {
		return u
	}
	return ""
}
