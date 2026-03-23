package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// LmfClaims holds JWT claims for LMF token validation
type LmfClaims struct {
	Scope  string `json:"scope"`
	PlmnId string `json:"plmnId,omitempty"`
	jwt.RegisteredClaims
}

// ValidateToken is a Gin middleware that validates OAuth 2.0 Bearer tokens
func ValidateToken(signingKey []byte, requiredScope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"title":  "Unauthorized",
				"status": 401,
				"detail": "Authorization header missing",
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"title":  "Unauthorized",
				"status": 401,
				"detail": "Invalid Authorization header format, expected 'Bearer <token>'",
			})
			return
		}

		tokenString := parts[1]

		claims := &LmfClaims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, jwt.ErrSignatureInvalid
			}
			return signingKey, nil
		})

		if err != nil || !token.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"title":  "Unauthorized",
				"status": 401,
				"detail": "Invalid or expired token",
			})
			return
		}

		// Check required scope
		if requiredScope != "" && !strings.Contains(claims.Scope, requiredScope) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"title":  "Forbidden",
				"status": 403,
				"detail": "Insufficient scope",
			})
			return
		}

		// Store claims in context for downstream use
		c.Set("tokenClaims", claims)
		c.Set("subject", claims.Subject)

		c.Next()
	}
}
