package middleware

import (
	"errors"
	"omnirelay/internal/apiresponse"
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

		errFmt := apiresponse.FormatFromContext(c)

		if apiKeyValue == "" {
			apiresponse.AbortUnauthorized(c, errFmt, "missing Authorization header")
			return
		}

		apiKey, err := svc.Validate(apiKeyValue)
		if err != nil {
			if errors.Is(err, service.ErrRateLimitExceeded) {
				apiresponse.AbortRateLimited(c, errFmt, err.Error())
				return
			}
			apiresponse.AbortUnauthorized(c, errFmt, err.Error())
			return
		}

		c.Set("api_key_id", apiKey.ID)
		c.Set("api_key_name", apiKey.Name)
		c.Set("user_id", apiKey.CreatedBy)
		c.Next()
	}
}
