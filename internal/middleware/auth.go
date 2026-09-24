package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// RoleAdmin may write showtimes; every other role is read-only.
const RoleAdmin = "admin"

// Claims is the JWT payload issued at login: the user ID in "sub" and their role.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

type claimsContextKey struct{}

// Auth rejects a request without a valid "Authorization: Bearer <jwt>" with 401,
// and stores the token's claims in the request context for ClaimsFrom.
func Auth(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme, token, _ := strings.Cut(c.GetHeader("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || token == "" { // the scheme is case-insensitive (RFC 9110)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "missing bearer token"})
			return
		}

		var claims Claims
		_, err := jwt.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) { return secret, nil },
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
		if err != nil {
			_ = c.Error(err)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "invalid or expired token"})
			return
		}

		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), claimsContextKey{}, claims))
		c.Next()
	}
}

// ClaimsFrom returns the caller's claims, or the zero value outside an authenticated request.
func ClaimsFrom(ctx context.Context) Claims {
	claims, _ := RequestContext(ctx).Value(claimsContextKey{}).(Claims)
	return claims
}
