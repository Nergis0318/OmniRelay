package handlers

import (
	"errors"
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/proxy"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func ListProviders(svc *service.ProviderService, authSvc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetInt64("user_id")
		providers, err := svc.List(userID)
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		if !c.GetBool("is_admin") {
			allowed, err := authSvc.AllowedProviderSet(userID)
			if err != nil {
				apiresponse.AbortAdminInternal(c, err.Error())
				return
			}
			if allowed != nil {
				filtered := providers[:0]
				for _, p := range providers {
					if allowed[p.ID] {
						filtered = append(filtered, p)
					}
				}
				providers = filtered
			}
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
			var pe *service.ProviderError
			if errors.As(err, &pe) {
				apiresponse.AbortAdminError(c, pe.StatusCode, err.Error(), "")
				return
			}
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

func TestProvider(ps *service.ProviderService, proxyEngine *proxy.Engine) gin.HandlerFunc {
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

		if provider.ProviderType == "custom" {
			apiresponse.AbortAdminError(c, 400, "cannot test custom provider", "")
			return
		}

		apiKey, err := ps.DecryptAPIKey(provider.APIKeyEncrypted)
		if err != nil {
			apiresponse.AbortAdminInternal(c, "failed to decrypt provider key")
			return
		}

		modelID, err := ps.FirstModelID(id, userID)
		if err != nil {
			apiresponse.AbortAdminInternal(c, "failed to find a model for this provider")
			return
		}

		if modelID == "" {
			apiresponse.AbortAdminError(c, 400, "no models available. Sync models first.", "")
			return
		}

		result := proxyEngine.TestProvider(provider, apiKey, modelID)
		c.JSON(http.StatusOK, result)
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
			apiresponse.AbortAdminError(c, http.StatusBadGateway, "failed to fetch models: "+err.Error(), "bad_gateway")
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

