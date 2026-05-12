package handlers

import (
	"net/http"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListModels(svc *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		providerKey := c.Query("provider_key")

		modelList, err := svc.List(providerKey)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
		var req models.CreateModelRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		model, err := svc.Create(req)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"model": model})
	}
}

func UpdateModel(svc *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model ID"})
			return
		}

		var req models.UpdateModelRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		model, err := svc.Update(id, req)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"model": model})
	}
}

func DeleteModel(svc *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid model ID"})
			return
		}

		if err := svc.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "model deleted"})
	}
}
