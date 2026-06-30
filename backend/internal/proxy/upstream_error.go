package proxy

import (
	"encoding/json"
	"omnirelay/internal/apiresponse"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// upstreamError is a provider-agnostic representation of an upstream error.
type upstreamError struct {
	ErrType string
	Message string
	Code    string
	Param   string
}

// parseUpstreamError extracts a normalized error from a provider's raw error JSON body.
// Returns (upstreamError, true) on success, (_, false) if the body is not a recognized shape.
func parseUpstreamError(providerType string, body []byte) (upstreamError, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return upstreamError{}, false
	}

	switch providerType {
	case "anthropic":
		return parseAnthropicError(raw)
	case "gemini":
		return parseGeminiError(raw)
	case "openai", "lmstudio", "ollama":
		return parseOpenAIError(raw)
	default:
		return parseGenericError(raw)
	}
}

func parseOpenAIError(raw map[string]interface{}) (upstreamError, bool) {
	errObj, ok := raw["error"].(map[string]interface{})
	if !ok {
		return parseGenericError(raw)
	}
	e := upstreamError{
		ErrType: vstr(errObj, "type"),
		Message: vstr(errObj, "message"),
		Code:    vstr(errObj, "code"),
		Param:   vstr(errObj, "param"),
	}
	if e.Message == "" {
		return upstreamError{}, false
	}
	return e, true
}

func parseAnthropicError(raw map[string]interface{}) (upstreamError, bool) {
	errObj, ok := raw["error"].(map[string]interface{})
	if !ok {
		return parseGenericError(raw)
	}
	e := upstreamError{
		ErrType: vstr(errObj, "type"),
		Message: vstr(errObj, "message"),
	}
	if e.Message == "" {
		return upstreamError{}, false
	}
	return e, true
}

func parseGeminiError(raw map[string]interface{}) (upstreamError, bool) {
	errObj, ok := raw["error"].(map[string]interface{})
	if !ok {
		return parseGenericError(raw)
	}
	e := upstreamError{
		Message: vstr(errObj, "message"),
		Code:    vstr(errObj, "status"),
	}
	if e.Code == "" {
		if i, ok := errObj["code"].(float64); ok {
			e.Code = strconv.FormatInt(int64(i), 10)
		}
	}
	if e.Message == "" {
		return upstreamError{}, false
	}
	return e, true
}

func parseGenericError(raw map[string]interface{}) (upstreamError, bool) {
	if errObj, ok := raw["error"].(map[string]interface{}); ok {
		if msg := vstr(errObj, "message"); msg != "" {
			return upstreamError{
				ErrType: vstr(errObj, "type"),
				Message: msg,
				Code:    vstr(errObj, "code"),
				Param:   vstr(errObj, "param"),
			}, true
		}
	}
	if msg, ok := raw["message"].(string); ok && msg != "" {
		return upstreamError{Message: msg}, true
	}
	return upstreamError{}, false
}

// reformatError serializes a normalized error into the target format's wire shape,
// including the request_id. Returns JSON-encoded bytes.
func reformatError(err upstreamError, targetFormat apiresponse.Format, requestID string) []byte {
	switch targetFormat {
	case apiresponse.FormatAnthropic:
		obj := map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    err.ErrType,
				"message": err.Message,
			},
			"request_id": requestID,
		}
		b, _ := json.Marshal(obj)
		return b
	default:
		obj := map[string]interface{}{
			"error": map[string]interface{}{
				"message":    err.Message,
				"type":       err.ErrType,
				"param":      nil,
				"code":       nil,
				"request_id": requestID,
			},
		}
		if err.Param != "" {
			obj["error"].(map[string]interface{})["param"] = err.Param
		}
		if err.Code != "" {
			obj["error"].(map[string]interface{})["code"] = err.Code
		}
		b, _ := json.Marshal(obj)
		return b
	}
}

// ensureRequestID reads the X-Request-Id header or generates a new UUID,
// stores it in the gin context, and returns it.
func ensureRequestID(c *gin.Context) string {
	if id := c.GetHeader("X-Request-Id"); id != "" {
		c.Set("request_id", id)
		return id
	}
	id := uuid.New().String()
	c.Set("request_id", id)
	return id
}

// vstr safely extracts a string value from a map.
func vstr(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
