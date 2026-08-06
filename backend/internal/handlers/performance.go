package handlers

import (
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func GetPerformance(svc *service.PerformanceService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		var params models.PerformanceQueryParams
		if v := c.Query("provider_id"); v != "" {
			id, _ := strconv.ParseInt(v, 10, 64)
			params.ProviderID = &id
		}
		params.From = c.Query("from")
		params.To = c.Query("to")
		params.Granularity = c.Query("granularity")

		resp, err := svc.GetPerformance(userID, params)
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, resp)
	}
}
