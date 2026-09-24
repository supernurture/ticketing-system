package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// RoleAdmin may write showtimes; every other role is read-only.
const RoleAdmin = "admin"

// Claims is the JWT payload: user ID in "sub" plus role.
type Claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

// UserID returns "sub" as a number, or 0.
func (c Claims) UserID() int64 {
	id, _ := strconv.ParseInt(c.Subject, 10, 64)
	return id
}

type claimsContextKey struct{}

// Auth answers 401 without a valid bearer token and stores the claims for ClaimsFrom.
func Auth(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme, token, _ := strings.Cut(c.GetHeader("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || token == "" { // scheme is case-insensitive
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
