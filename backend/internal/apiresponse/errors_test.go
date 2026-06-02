package apiresponse

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAbortOpenAIErrorShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	AbortInvalidRequest(c, FormatOpenAI, "you must provide a messages parameter", "messages")

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", resp)
	}
	for _, key := range []string{"message", "type", "param", "code"} {
		if _, ok := errObj[key]; !ok {
			t.Errorf("missing error.%s", key)
		}
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("type = %v", errObj["type"])
	}
	if errObj["param"] != "messages" {
		t.Errorf("param = %v", errObj["param"])
	}
}

func TestAbortAnthropicErrorShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	AbortInvalidRequest(c, FormatAnthropic, "max_tokens: Field required", "max_tokens")

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["type"] != "error" {
		t.Errorf("type = %v, want error", resp["type"])
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error object missing: %v", resp)
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("error.type = %v", errObj["type"])
	}
	if _, ok := resp["request_id"]; !ok {
		t.Error("request_id missing")
	}
}

func TestFormatFromPath(t *testing.T) {
	if FormatFromPath("/v1/chat/completions") != FormatOpenAI {
		t.Error("chat completions should be OpenAI format")
	}
	if FormatFromPath("/anthropic/v1/messages") != FormatAnthropic {
		t.Error("path-routed messages should be Anthropic format")
	}
}