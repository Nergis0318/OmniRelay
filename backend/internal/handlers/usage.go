package handlers

import (
	"net/http"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListUsage(svc *service.UsageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		var params models.UsageQueryParams

		if v := c.Query("api_key_id"); v != "" {
			id, _ := strconv.ParseInt(v, 10, 64)
			params.APIKeyID = &id
		}
		if v := c.Query("provider_id"); v != "" {
			id, _ := strconv.ParseInt(v, 10, 64)
			params.ProviderID = &id
		}
		params.Model = c.Query("model")
		params.From = c.Query("from")
		params.To = c.Query("to")
		if v := c.Query("limit"); v != "" {
			params.Limit, _ = strconv.Atoi(v)
		}
		if v := c.Query("offset"); v != "" {
			params.Offset, _ = strconv.Atoi(v)
		}

		logs, total, err := svc.Query(params, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if logs == nil {
			logs = []models.UsageLog{}
		}

		c.JSON(http.StatusOK, gin.H{
			"usage_logs": logs,
			"total":      total,
			"limit":      params.Limit,
			"offset":     params.Offset,
		})
	}
}

func GetStats(us *service.UsageService, ks *service.APIKeyService, ms *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		stats, err := us.GetStats(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		stats.ActiveKeys, err = ks.CountActive(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		stats.ModelsCount, err = ms.CountActive(userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, stats)
	}
}
