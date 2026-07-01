package handlers

import (
	"net/http"
	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"
	"omnirelay/internal/service"

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
			apiresponse.AbortAdminUnauthorized(c, err.Error())
			return
		}

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
