package middleware

import (
	"net/http"
	"omnirelay/internal/service"
	"strings"

	"github.com/gin-gonic/gin"
)

func APIKeyAuth(svc *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
			c.Abort()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid Authorization header format"})
			c.Abort()
			return
		}

		apiKey, err := svc.Validate(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		c.Set("api_key_id", apiKey.ID)
		c.Set("api_key_name", apiKey.Name)
		c.Next()
	}
}
