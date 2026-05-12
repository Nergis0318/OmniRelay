package handlers

import (
	"net/http"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListAPIKeys(svc *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		keys, err := svc.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if keys == nil {
			keys = []models.APIKey{}
		}
		c.JSON(http.StatusOK, gin.H{"api_keys": keys})
	}
}

func CreateAPIKey(svc *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.CreateAPIKeyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		userID := c.GetInt64("user_id")

		resp, err := svc.Create(req, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, resp)
	}
}

func DeleteAPIKey(svc *service.APIKeyService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid API key ID"})
			return
		}

		if err := svc.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "API key deactivated"})
	}
}
