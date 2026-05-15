package middleware

import (
	"errors"
	"net/http"
	"omnirelay/internal/service"
	"strings"

	"github.com/gin-gonic/gin"
)

func APIKeyAuth(svc *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var apiKeyValue string

		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && parts[0] == "Bearer" {
				apiKeyValue = parts[1]
			}
		}

		if apiKeyValue == "" {
			apiKeyValue = c.GetHeader("x-api-key")
		}

		if apiKeyValue == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "missing Authorization header"})
			c.Abort()
			return
		}

		apiKey, err := svc.Validate(apiKeyValue)
		if err != nil {
			if errors.Is(err, service.ErrRateLimitExceeded) {
				c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
				c.Abort()
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			c.Abort()
			return
		}

		c.Set("api_key_id", apiKey.ID)
		c.Set("api_key_name", apiKey.Name)
		c.Set("user_id", apiKey.CreatedBy)
		c.Next()
	}
}
