package handlers

import (
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
	"strconv"

	"github.com/gin-gonic/gin"
)

func Register(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.RegisterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}

		user, err := svc.Register(req)
		if err != nil {
			apiresponse.AbortAdminConflict(c, err.Error())
			return
		}

		c.JSON(http.StatusCreated, gin.H{"user": user})
	}
}

func Login(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.LoginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}

		resp, err := svc.Login(req)
		if err != nil {
			apiresponse.AbortAdminError(c, http.StatusUnauthorized, err.Error(), "unauthorized")
			return
		}

		resetLoginRateLimit(c)
		c.JSON(http.StatusOK, resp)
	}
}

func ListUsers(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		users, err := svc.ListUsers()
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"users": users})
	}
}

func DeleteUser(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid user id")
			return
		}
		requesterID := c.GetInt64("user_id")
		if err := svc.DeleteUser(id, requesterID); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "user deleted"})
	}
}

func SetUserRole(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid user id")
			return
		}
		var req models.SetRoleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}
		if err := svc.SetRole(id, req.IsAdmin); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "role updated"})
	}
}

func GenerateResetCode(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid user id")
			return
		}
		code, err := svc.GenerateResetCode(id)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"code": code})
	}
}

func ResetPassword(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req models.ResetPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}
		if err := svc.ResetPasswordWithCode(req.Code, req.NewPassword); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "password reset successful"})
	}
}

func GetUserProviders(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid user id")
			return
		}
		ids, err := svc.GetUserProviders(id)
		if err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		if ids == nil {
			ids = []int64{}
		}
		c.JSON(http.StatusOK, gin.H{"provider_ids": ids})
	}
}

func SetUserProviders(svc *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			apiresponse.AbortAdminBadRequest(c, "invalid user id")
			return
		}
		var req models.SetUserProvidersRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.AbortAdminBadRequest(c, err.Error())
			return
		}
		if err := svc.SetUserProviders(id, req.ProviderIDs); err != nil {
			apiresponse.AbortAdminInternal(c, err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "providers updated"})
	}
}
