package handlers

import (
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

// GetPassthroughPerformance reports relay stats for URL-passthrough traffic,
// including the usage the upstream reported on its own responses. These records
// live outside usage_logs and never carry cost.
func GetPassthroughPerformance(svc *service.PassthroughService) gin.HandlerFunc {
	return func(c *gin.Context) {
		resp, err := svc.GetPerformance(passthroughQueryParams(c))
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}

// ListPassthroughLogs returns the most recent individual relay measurements.
func ListPassthroughLogs(svc *service.PassthroughService) gin.HandlerFunc {
	return func(c *gin.Context) {
		params := passthroughQueryParams(c)
		params.Limit, _ = strconv.Atoi(c.DefaultQuery("limit", "100"))
		params.Offset, _ = strconv.Atoi(c.DefaultQuery("offset", "0"))

		logs, total, err := svc.List(params)
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": logs, "total": total, "limit": params.Limit, "offset": params.Offset})
	}
}

func passthroughQueryParams(c *gin.Context) models.PassthroughQueryParams {
	return models.PassthroughQueryParams{
		Host:        c.Query("host"),
		From:        c.Query("from"),
		To:          c.Query("to"),
		Granularity: c.Query("granularity"),
	}
}
