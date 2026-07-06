package proxy

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const defaultAnthropicVersion = "2023-06-01"

func joinUpstreamURL(baseURL, endpoint string) string {
	if endpoint == "" {
		return strings.TrimRight(baseURL, "/")
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}
	return strings.TrimRight(baseURL, "/") + endpoint
}

func isSuccessStatus(status int) bool {
	return status >= http.StatusOK && status < http.StatusMultipleChoices
}

func setProviderAuthHeaders(req *http.Request, providerType, apiKey string) {
	switch providerType {
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		if req.Header.Get("anthropic-version") == "" {
			req.Header.Set("anthropic-version", defaultAnthropicVersion)
		}
	case "gemini":
		req.Header.Set("x-goog-api-key", apiKey)
	case "ollama":
		return
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

func copyForwardableRequestHeaders(c *gin.Context, req *http.Request) {
	connectionHeaders := map[string]struct{}{}
	for _, value := range c.Request.Header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				connectionHeaders[http.CanonicalHeaderKey(name)] = struct{}{}
			}
		}
	}

	for name, values := range c.Request.Header {
		if !isForwardableRequestHeader(name, connectionHeaders) {
			continue
		}
		req.Header.Del(name)
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
}

func isForwardableRequestHeader(name string, connectionHeaders map[string]struct{}) bool {
	canonicalName := http.CanonicalHeaderKey(name)
	if _, ok := connectionHeaders[canonicalName]; ok {
		return false
	}

	switch strings.ToLower(name) {
	case "authorization", "connection", "content-length", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade", "x-api-key", "x-goog-api-key":
		return false
	default:
		return true
	}
}

func copyResponseHeaders(c *gin.Context, header http.Header) {
	for k, values := range header {
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Content-Encoding") || strings.EqualFold(k, "Transfer-Encoding") {
			continue
		}
		for _, v := range values {
			c.Header(k, v)
		}
	}
}

func contentTypeOrDefault(header http.Header) string {
	if contentType := header.Get("Content-Type"); contentType != "" {
		return contentType
	}
	return "application/json"
}

func routeAPIPrefix(path string) string {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func routedBaseURL(providerType, apiPrefix, baseURL string) string {
	if providerType == "ollama" && apiPrefix == "api" {
		return trimPathSuffix(baseURL, "/v1")
	}
	return baseURL
}

func routedEndpoint(apiPrefix, baseURL, endpoint string) string {
	if endpoint == "" {
		endpoint = "/"
	}
	if !strings.HasPrefix(endpoint, "/") {
		endpoint = "/" + endpoint
	}

	prefix := "/" + apiPrefix
	if apiPrefix == "" || hasPathSuffix(baseURL, prefix) {
		return endpoint
	}
	return prefix + endpoint
}

func appendRawQuery(upstreamURL, rawQuery string) string {
	if rawQuery == "" {
		return upstreamURL
	}
	if strings.Contains(upstreamURL, "?") {
		return upstreamURL + "&" + rawQuery
	}
	return upstreamURL + "?" + rawQuery
}

func isJSONContentType(contentType string) bool {
	contentType = strings.ToLower(contentType)
	return contentType == "" || strings.Contains(contentType, "application/json")
}

func hasPathSuffix(rawURL, suffix string) bool {
	return strings.HasSuffix(strings.TrimRight(rawURL, "/"), suffix)
}

func trimPathSuffix(rawURL, suffix string) string {
	trimmed := strings.TrimRight(rawURL, "/")
	if strings.HasSuffix(trimmed, suffix) {
		return strings.TrimSuffix(trimmed, suffix)
	}
	return rawURL
}

func stripProviderPrefix(modelID string) string {
	if idx := strings.IndexByte(modelID, '/'); idx >= 0 {
		return modelID[idx+1:]
	}
	return modelID
}

