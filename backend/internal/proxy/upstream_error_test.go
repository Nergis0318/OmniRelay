package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"omnirelay/internal/apiresponse"
)

func TestParseUpstreamError_OpenAI(t *testing.T) {
	body := []byte(`{"error":{"message":"You exceeded your current quota","type":"rate_limit_exceeded","param":null,"code":"rate_limit_exceeded"}}`)
	parsed, ok := parseUpstreamError("openai", body)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if parsed.Message != "You exceeded your current quota" {
		t.Errorf("message = %q", parsed.Message)
	}
	if parsed.Code != "rate_limit_exceeded" {
		t.Errorf("code = %q", parsed.Code)
	}
}

func TestParseUpstreamError_Anthropic(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
	parsed, ok := parseUpstreamError("anthropic", body)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if parsed.Message != "rate limited" {
		t.Errorf("message = %q", parsed.Message)
	}
	if parsed.ErrType != "rate_limit_error" {
		t.Errorf("errType = %q", parsed.ErrType)
	}
}

func TestParseUpstreamError_Gemini(t *testing.T) {
	body := []byte(`{"error":{"code":429,"message":"Quota exceeded","status":"RESOURCE_EXHAUSTED"}}`)
	parsed, ok := parseUpstreamError("gemini", body)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if parsed.Message != "Quota exceeded" {
		t.Errorf("message = %q", parsed.Message)
	}
	if parsed.Code != "429" && parsed.Code != "RESOURCE_EXHAUSTED" {
		t.Errorf("code = %q", parsed.Code)
	}
}

func TestParseUpstreamError_Fallback(t *testing.T) {
	body := []byte(`{"message":"something broke"}`)
	parsed, ok := parseUpstreamError("unknown", body)
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if parsed.Message != "something broke" {
		t.Errorf("message = %q", parsed.Message)
	}
}

func TestParseUpstreamError_InvalidJSON(t *testing.T) {
	body := []byte(`not json`)
	_, ok := parseUpstreamError("openai", body)
	if ok {
		t.Error("expected parse to fail on invalid JSON")
	}
}

func TestReformatError_OpenAI(t *testing.T) {
	err := upstreamError{ErrType: "rate_limit_exceeded", Message: "quota exceeded", Code: "rate_limit_exceeded"}
	result := reformatError(err, apiresponse.FormatOpenAI, "test-req-123")
	var resp map[string]interface{}
	if e := json.Unmarshal(result, &resp); e != nil {
		t.Fatal(e)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", resp)
	}
	if errObj["request_id"] != "test-req-123" {
		t.Errorf("request_id = %v", errObj["request_id"])
	}
	if errObj["type"] != "rate_limit_exceeded" {
		t.Errorf("type = %v", errObj["type"])
	}
}

func TestReformatError_Anthropic(t *testing.T) {
	err := upstreamError{ErrType: "rate_limit_error", Message: "rate limited"}
	result := reformatError(err, apiresponse.FormatAnthropic, "test-req-456")
	var resp map[string]interface{}
	if e := json.Unmarshal(result, &resp); e != nil {
		t.Fatal(e)
	}
	if resp["type"] != "error" {
		t.Errorf("type = %v", resp["type"])
	}
	if resp["request_id"] != "test-req-456" {
		t.Errorf("request_id = %v", resp["request_id"])
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", resp)
	}
	if errObj["type"] != "rate_limit_error" {
		t.Errorf("error.type = %v", errObj["type"])
	}
}

func TestEnsureRequestID_GeneratesUUID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	id := ensureRequestID(c)
	if id == "" {
		t.Error("expected non-empty request_id")
	}
	if c.GetString("request_id") != id {
		t.Error("request_id not stored in context")
	}
}

func TestEnsureRequestID_RespectsHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Request.Header.Set("X-Request-Id", "client-provided-123")

	id := ensureRequestID(c)
	if id != "client-provided-123" {
		t.Errorf("expected client-provided ID, got %q", id)
	}
}
