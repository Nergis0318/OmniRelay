package handlers

import (
	"net/http"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListProviders(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		providers, err := svc.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if providers == nil {
			providers = []models.Provider{}
		}
		c.JSON(http.StatusOK, gin.H{"providers": providers})
	}
}

func CreateProvider(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.CreateProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		provider, err := svc.Create(req)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusCreated, gin.H{"provider": provider})
	}
}

func UpdateProvider(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider ID"})
			return
		}

		var req models.UpdateProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		provider, err := svc.Update(id, req)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"provider": provider})
	}
}

func DeleteProvider(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider ID"})
			return
		}

		if err := svc.Delete(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "provider deleted"})
	}
}

func SyncProviderModels(ps *service.ProviderService, ms *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider ID"})
			return
		}

		provider, err := ps.GetByID(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
			return
		}

		modelIDs, err := ps.FetchModelsFromProvider(provider)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch models: " + err.Error()})
			return
		}

		if err := ms.SyncFromProvider(provider.ID, provider.ProviderKey, modelIDs); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync models: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":   "models synced",
			"model_count": len(modelIDs),
		})
	}
}
