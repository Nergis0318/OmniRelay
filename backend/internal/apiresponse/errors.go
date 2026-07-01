package apiresponse

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Format selects provider-native error JSON for proxy routes.
type Format int

const (
	FormatOpenAI Format = iota
	FormatAnthropic
)

// FormatFromPath picks OpenAI-style vs Anthropic-style errors from the request path.
func FormatFromPath(path string) Format {
	path = strings.TrimSuffix(path, "/")
	if strings.HasSuffix(path, "/messages") {
		return FormatAnthropic
	}
	return FormatOpenAI
}

// FormatFromContext uses the incoming request URL path.
func FormatFromContext(c *gin.Context) Format {
	return FormatFromPath(c.Request.URL.Path)
}

// Abort writes a spec-shaped error and stops the handler chain.
func Abort(c *gin.Context, status int, format Format, errType, message, code, param string) {
	switch format {
	case FormatAnthropic:
		abortAnthropic(c, status, errType, message)
	default:
		abortOpenAI(c, status, errType, message, code, param)
	}
	c.Abort()
}

func abortOpenAI(c *gin.Context, status int, errType, message, code, param string) {
	if errType == "" {
		errType = "invalid_request_error"
	}
	errObj := gin.H{
		"message": message,
		"type":    errType,
		"param":   nil,
		"code":    nil,
	}
	if param != "" {
		errObj["param"] = param
	}
	if code != "" {
		errObj["code"] = code
	}
	if requestID := c.GetString("request_id"); requestID != "" {
		errObj["request_id"] = requestID
	}
	c.JSON(status, gin.H{"error": errObj})
}

func abortAnthropic(c *gin.Context, status int, errType, message string) {
	if errType == "" {
		errType = "invalid_request_error"
	}
	c.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    errType,
			"message": message,
		},
		"request_id": c.GetString("request_id"),
	})
}

// AbortInvalidRequest is a convenience for 400 invalid_request_error.
func AbortInvalidRequest(c *gin.Context, format Format, message, param string) {
	Abort(c, http.StatusBadRequest, format, "invalid_request_error", message, "missing_required_parameter", param)
}

// AbortUnauthorized writes authentication errors in the appropriate format.
func AbortUnauthorized(c *gin.Context, format Format, message string) {
	switch format {
	case FormatAnthropic:
		Abort(c, http.StatusUnauthorized, format, "authentication_error", message, "", "")
	default:
		Abort(c, http.StatusUnauthorized, format, "invalid_request_error", message, "invalid_api_key", "")
	}
}

// AbortRateLimited writes rate-limit errors in the appropriate format.
func AbortRateLimited(c *gin.Context, format Format, message string) {
	switch format {
	case FormatAnthropic:
		Abort(c, http.StatusTooManyRequests, format, "rate_limit_error", message, "", "")
	default:
		Abort(c, http.StatusTooManyRequests, format, "rate_limit_exceeded", message, "rate_limit_exceeded", "")
	}
}

// AbortNotFound writes not-found errors (OpenAI model param when applicable).
func AbortNotFound(c *gin.Context, format Format, message, param string) {
	switch format {
	case FormatAnthropic:
		Abort(c, http.StatusNotFound, format, "not_found_error", message, "", "")
	default:
		Abort(c, http.StatusNotFound, format, "invalid_request_error", message, "model_not_found", param)
	}
}

// AbortInternal writes server-side gateway errors.
func AbortInternal(c *gin.Context, format Format, message string) {
	switch format {
	case FormatAnthropic:
		Abort(c, http.StatusInternalServerError, format, "api_error", message, "", "")
	default:
		Abort(c, http.StatusInternalServerError, format, "server_error", message, "internal_error", "")
	}
}

// AbortBadGateway writes upstream connectivity failures.
func AbortBadGateway(c *gin.Context, format Format, message string) {
	switch format {
	case FormatAnthropic:
		Abort(c, http.StatusBadGateway, format, "api_error", message, "", "")
	default:
		Abort(c, http.StatusBadGateway, format, "server_error", message, "upstream_error", "")
	}
}

// AbortForbidden writes forbidden errors in the appropriate format.
func AbortForbidden(c *gin.Context, format Format, message string) {
	switch format {
	case FormatAnthropic:
		Abort(c, http.StatusForbidden, format, "permission_error", message, "", "")
	default:
		Abort(c, http.StatusForbidden, format, "permission_denied", message, "permission_denied", "")
	}
}

// --- Admin (dashboard) API error helpers ---

// adminErrorBody returns a consistent JSON body for admin API errors.
func adminErrorBody(message, code string) gin.H {
	body := gin.H{"error": message}
	if code != "" {
		body["code"] = code
	}
	return body
}

// AbortAdminError writes a standardized admin API error response.
func AbortAdminError(c *gin.Context, status int, message, code string) {
	c.AbortWithStatusJSON(status, adminErrorBody(message, code))
}

// AbortAdminBadRequest writes a 400 admin error.
func AbortAdminBadRequest(c *gin.Context, message string) {
	AbortAdminError(c, http.StatusBadRequest, message, "bad_request")
}

// AbortAdminNotFound writes a 404 admin error.
func AbortAdminNotFound(c *gin.Context, message string) {
	AbortAdminError(c, http.StatusNotFound, message, "not_found")
}

// AbortAdminConflict writes a 409 admin error.
func AbortAdminConflict(c *gin.Context, message string) {
	AbortAdminError(c, http.StatusConflict, message, "conflict")
}

// AbortAdminInternal writes a 500 admin error.
func AbortAdminInternal(c *gin.Context, message string) {
	AbortAdminError(c, http.StatusInternalServerError, message, "internal_error")
}

// AbortAdminBadGateway writes a 502 admin error.
func AbortAdminBadGateway(c *gin.Context, message string) {
	AbortAdminError(c, http.StatusBadGateway, message, "bad_gateway")
}

// AbortAdminUnauthorized writes a 401 admin error.
func AbortAdminUnauthorized(c *gin.Context, message string) {
	AbortAdminError(c, http.StatusUnauthorized, message, "unauthorized")
}