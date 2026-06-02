package apiresponse

import "fmt"

// ValidateChatCompletionBody checks OpenAPI CreateChatCompletionRequest required fields.
func ValidateChatCompletionBody(body map[string]interface{}) (param string, err error) {
	if _, ok := body["model"].(string); !ok {
		return "model", fmt.Errorf("you must provide a model parameter")
	}
	messages, ok := body["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return "messages", fmt.Errorf("you must provide a messages parameter")
	}
	return "", nil
}

// ValidateMessagesBody checks Anthropic CreateMessageParams required fields.
func ValidateMessagesBody(body map[string]interface{}) (param string, err error) {
	if _, ok := body["model"].(string); !ok {
		return "model", fmt.Errorf("model: Field required")
	}
	messages, ok := body["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return "messages", fmt.Errorf("messages: Field required")
	}
	if !hasPositiveNumber(body["max_tokens"]) {
		return "max_tokens", fmt.Errorf("max_tokens: Field required")
	}
	return "", nil
}

func hasPositiveNumber(v interface{}) bool {
	switch n := v.(type) {
	case float64:
		return n > 0
	case int:
		return n > 0
	case int64:
		return n > 0
	default:
		return false
	}
}