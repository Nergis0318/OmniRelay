package apiresponse

import (
	"encoding/json"
	"testing"
)

// Fixtures mirror minimal examples from OpenAPI-Specification/ (OpenAI.yml, Anthropic.yml).

func TestOpenAIChatCompletionRequestContract(t *testing.T) {
	body := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
		},
	}
	if param, err := ValidateChatCompletionBody(body); err != nil {
		t.Fatalf("valid OpenAI example should pass: param=%q err=%v", param, err)
	}
}

func TestAnthropicCreateMessageParamsContract(t *testing.T) {
	// Anthropic.yml CreateMessageParams example
	body := map[string]interface{}{
		"max_tokens": float64(1024),
		"messages": []interface{}{
			map[string]interface{}{"content": "Hello, world", "role": "user"},
		},
		"model": "claude-opus-4-6",
	}
	if param, err := ValidateMessagesBody(body); err != nil {
		t.Fatalf("valid Anthropic example should pass: param=%q err=%v", param, err)
	}
}

func TestOpenAIListModelsResponseContract(t *testing.T) {
	// OpenAI.yml list models example shape
	raw := `{
	  "object": "list",
	  "data": [
	    {
	      "id": "model-id-0",
	      "object": "model",
	      "created": 1686935002,
	      "owned_by": "organization-owner"
	    }
	  ]
	}`
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["object"] != "list" {
		t.Errorf("object = %v", resp["object"])
	}
	data, ok := resp["data"].([]interface{})
	if !ok || len(data) == 0 {
		t.Fatal("data array missing")
	}
	item, ok := data[0].(map[string]interface{})
	if !ok {
		t.Fatal("data item shape")
	}
	for _, key := range []string{"id", "object", "created", "owned_by"} {
		if _, ok := item[key]; !ok {
			t.Errorf("model missing %s", key)
		}
	}
}

func TestOpenAIErrorResponseContract(t *testing.T) {
	raw := `{
	  "error": {
	    "message": "you must provide a messages parameter",
	    "type": "invalid_request_error",
	    "param": "messages",
	    "code": "missing_required_parameter"
	  }
	}`
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("missing error object")
	}
	for _, key := range []string{"message", "type", "param", "code"} {
		if _, ok := errObj[key]; !ok {
			t.Errorf("error.%s missing", key)
		}
	}
}

func TestAnthropicErrorResponseContract(t *testing.T) {
	raw := `{
	  "type": "error",
	  "error": {
	    "type": "invalid_request_error",
	    "message": "max_tokens: Field required"
	  },
	  "request_id": null
	}`
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["type"] != "error" {
		t.Errorf("type = %v", resp["type"])
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("missing error object")
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("error.type = %v", errObj["type"])
	}
}