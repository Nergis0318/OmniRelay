package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHandleChatCompletionsRejectsMissingMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := &Engine{}

	r := gin.New()
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		e.HandleChatCompletions(c)
	})

	body := `{"model":"openai/gpt-4o"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected OpenAI error object, got %v", resp)
	}
	if errObj["param"] != "messages" {
		t.Errorf("param = %v, want messages", errObj["param"])
	}
}

func TestHandleMessagesRejectsMissingMaxTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := &Engine{}

	r := gin.New()
	r.POST("/v1/messages", func(c *gin.Context) {
		c.Set("api_key_id", int64(1))
		c.Set("user_id", int64(1))
		e.HandleMessages(c)
	})

	body := `{"model":"anthropic/claude","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["type"] != "error" {
		t.Fatalf("expected Anthropic error envelope, got %v", resp)
	}
}