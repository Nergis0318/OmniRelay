package handlers

import (
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListProviders(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		providers, err := svc.List(userID)
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
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
		userID := c.GetInt64("user_id")
		var req models.CreateProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}

		provider, err := svc.Create(req, userID)
		if err != nil {
			apiresponse.AbortAdminConflict(c, err.Error())
			return
		}

		c.JSON(http.StatusCreated, gin.H{"provider": provider})
	}
}

func UpdateProvider(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid provider ID")
			return
		}

		var req models.UpdateProviderRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}

		provider, err := svc.Update(id, userID, req)
		if err != nil {
			apiresponse.AbortAdminNotFound(c, err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{"provider": provider})
	}
}

func DeleteProvider(svc *service.ProviderService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid provider ID")
			return
		}

		if err := svc.Delete(id, userID); err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "provider deleted"})
	}
}

func SyncProviderModels(ps *service.ProviderService, ms *service.ModelService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid provider ID")
			return
		}

		provider, err := ps.GetByID(id, userID)
		if err != nil {
			apiresponse.AbortAdminNotFound(c, "provider not found")
			return
		}

		modelIDs, err := ps.FetchModelsFromProvider(provider)
		if err != nil {
			apiresponse.AbortAdminBadGateway(c, "failed to fetch models: "+err.Error())
			return
		}

		if err := ms.SyncFromProvider(provider.ID, provider.ProviderKey, modelIDs, userID); err != nil {
			apiresponse.AbortAdminInternal(c, "failed to sync models: "+err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":     "models synced",
			"model_count": len(modelIDs),
		})
	}
}

