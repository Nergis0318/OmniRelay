package handlers

import (
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListModels(svc *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		providerKey := c.Query("provider_key")

		modelList, err := svc.List(providerKey, userID)
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		if modelList == nil {
			modelList = []models.Model{}
		}
		c.JSON(http.StatusOK, gin.H{"models": modelList})
	}
}

func CreateModel(svc *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		var req models.CreateModelRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}

		model, err := svc.Create(req, userID)
		if err != nil {
			apiresponse.AbortAdminConflict(c, err.Error())
			return
		}

		c.JSON(http.StatusCreated, gin.H{"model": model})
	}
}

func UpdateModel(svc *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid model ID")
			return
		}

		var req models.UpdateModelRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}

		model, err := svc.Update(id, userID, req)
		if err != nil {
			apiresponse.AbortAdminNotFound(c, err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{"model": model})
	}
}

func ListSourceModels(svc *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		groups, err := svc.ListSourceModels(userID)
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		if groups == nil {
			groups = []service.SourceModelGroup{}
		}
		c.JSON(http.StatusOK, gin.H{"providers": groups})
	}
}

func DeleteModel(svc *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid model ID")
			return
		}

		if err := svc.Delete(id, userID); err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "model deleted"})
	}
}
