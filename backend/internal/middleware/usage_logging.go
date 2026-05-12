package middleware

import (
	"bytes"
	"omnirelay/internal/service"

	"github.com/gin-gonic/gin"
)

type responseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w *responseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func UsageLogging(svc *service.UsageService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}
