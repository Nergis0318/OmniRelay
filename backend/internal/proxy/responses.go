package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"omnirelay/internal/apiresponse"
	"omnirelay/internal/models"

	"github.com/gin-gonic/gin"
)

// responsesToChatBody converts an OpenAI Responses API request body into a
// chat completions request body for the existing proxy pipeline.
func responsesToChatBody(body map[string]interface{}) (map[string]interface{}, error) {
	chat := make(map[string]interface{})

	for _, key := range []string{"model", "stream", "temperature", "top_p", "stop"} {
		if v, ok := body[key]; ok {
			chat[key] = v
		}
	}
	if v, ok := body["max_output_tokens"]; ok {
		chat["max_tokens"] = v
	}

	messages, err := inputToMessages(body["input"])
	if err != nil {
		return nil, err
	}
	if instructions, ok := body["instructions"].(string); ok && instructions != "" {
		messages = append([]interface{}{map[string]interface{}{"role": "system", "content": instructions}}, messages...)
	}
	chat["messages"] = messages

	if tools, ok := body["tools"].([]interface{}); ok {
		chat["tools"] = convertResponsesTools(tools)
	}
	return chat, nil
}

func inputToMessages(input interface{}) ([]interface{}, error) {
	if s, ok := input.(string); ok {
		return []interface{}{map[string]interface{}{"role": "user", "content": s}}, nil
	}
	items, ok := input.([]interface{})
	if !ok {
		return nil, fmt.Errorf("input must be a string or an array")
	}
	var messages []interface{}
	for _, raw := range items {
		item, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		if msgType, _ := item["type"].(string); msgType != "" && msgType != "message" {
			switch msgType {
			case "function_call_output":
				messages = append(messages, map[string]interface{}{
					"role":         "tool",
					"tool_call_id": item["call_id"],
					"content":      item["output"],
				})
			case "function_call":
				args := "{}"
				if a, ok := item["arguments"]; ok {
					if s, isStr := a.(string); isStr {
						args = s
					} else if b, err := json.Marshal(a); err == nil {
						args = string(b)
					}
				}
				messages = append(messages, map[string]interface{}{
					"role": "assistant",
					"tool_calls": []map[string]interface{}{{
						"id":   item["call_id"],
						"type": "function",
						"function": map[string]interface{}{
							"name":      item["name"],
							"arguments": args,
						},
					}},
				})
			}
			continue
		}
		role, _ := item["role"].(string)
		if role == "" {
			role = "user"
		}
		messages = append(messages, map[string]interface{}{
			"role":    role,
			"content": convertResponsesContent(item["content"]),
		})
	}
	return messages, nil
}

func convertResponsesContent(content interface{}) interface{} {
	if s, ok := content.(string); ok {
		return s
	}
	parts, ok := content.([]interface{})
	if !ok {
		return ""
	}
	out := make([]map[string]interface{}, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		switch part["type"] {
		case "input_text":
			out = append(out, map[string]interface{}{"type": "text", "text": part["text"]})
		case "input_image":
			imageURL := map[string]interface{}{"url": part["image_url"]}
			if detail, ok := part["detail"]; ok {
				imageURL["detail"] = detail
			}
			out = append(out, map[string]interface{}{"type": "image_url", "image_url": imageURL})
		default:
			out = append(out, part)
		}
	}
	return out
}

func convertResponsesTools(tools []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]interface{})
		if !ok || tool["type"] != "function" {
			continue
		}
		fn := make(map[string]interface{})
		for _, k := range []string{"name", "description", "parameters", "strict"} {
			if v, ok := tool[k]; ok {
				fn[k] = v
			}
		}
		out = append(out, map[string]interface{}{"type": "function", "function": fn})
	}
	return out
}

func randomID(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return prefix + "000000000000"
	}
	return prefix + hex.EncodeToString(b)
}

// HandleResponses serves the OpenAI Responses API (/v1/responses) by
// translating requests into the chat completions pipeline and back.
func (e *Engine) HandleResponses(c *gin.Context) {
	ensureRequestID(c)
	apiKeyID := c.GetInt64("api_key_id")
	userID := c.GetInt64("user_id")

	body, ok := readJSONBody(c)
	if !ok {
		return
	}
	if param, err := apiresponse.ValidateResponsesBody(body); err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatOpenAI, err.Error(), param)
		return
	}

	fullModelID := body["model"].(string)

	dbModel, provider, adapter, _, ok := e.resolveDispatch(c, fullModelID, userID, apiresponse.FormatOpenAI, apiFormatOpenAI)
	if !ok {
		return
	}

	chatBody, err := responsesToChatBody(body)
	if err != nil {
		apiresponse.AbortInvalidRequest(c, apiresponse.FormatOpenAI, err.Error(), "")
		return
	}

	resp, startTime, inputTokens, wroteError := e.buildAndSendChatRequest(c, provider, dbModel, adapter, chatBody, fullModelID, apiKeyID, userID)
	if wroteError {
		return
	}
	defer resp.Body.Close()

	if extractStreamFlag(chatBody) {
		e.handleResponsesStream(c, resp, adapter, apiKeyID, provider.ID, fullModelID, dbModel, userID, provider.ProviderType, inputTokens)
		return
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		e.usageService.Log(models.UsageLog{
			APIKeyID:     &apiKeyID,
			ProviderID:   &provider.ID,
			Model:        fullModelID,
			IsError:      true,
			ErrorMessage: "failed to read the model response",
			UserID:       &userID,
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to read the model response"})
		return
	}

	chatResp, wrote := parseNonStreamChatResponse(c, respBody, resp.Header, adapter, fullModelID, dbModel, apiKeyID, provider.ID, startTime, userID, e.usageService, provider.ProviderType, inputTokens)
	if wrote || chatResp == nil {
		return
	}
	c.JSON(http.StatusOK, chatResponseToResponses(chatResp, fullModelID))
}

// chatResponseToResponses converts a chat completions response map into the
// OpenAI Responses API response shape.
func chatResponseToResponses(chatResp map[string]interface{}, fullModelID string) map[string]interface{} {
	var output []map[string]interface{}
	var outputText strings.Builder

	choices, _ := chatResp["choices"].([]interface{})
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]interface{})
		msg, _ := choice["message"].(map[string]interface{})

		if content, ok := msg["content"].(string); ok && content != "" {
			outputText.WriteString(content)
			output = append(output, map[string]interface{}{
				"id":     randomID("msg_"),
				"type":   "message",
				"status": "completed",
				"role":   "assistant",
				"content": []map[string]interface{}{
					{"type": "output_text", "text": content, "annotations": []interface{}{}},
				},
			})
		}

		if toolCalls, ok := msg["tool_calls"].([]interface{}); ok {
			for _, raw := range toolCalls {
				tc, _ := raw.(map[string]interface{})
				fn, _ := tc["function"].(map[string]interface{})
				callID, _ := tc["id"].(string)
				if callID == "" {
					callID = randomID("call_")
				}
				output = append(output, map[string]interface{}{
					"id":        randomID("fc_"),
					"type":      "function_call",
					"call_id":   callID,
					"name":      fn["name"],
					"arguments": fn["arguments"],
				})
			}
		}
	}

	status := "completed"
	if len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if fr, _ := choice["finish_reason"].(string); fr == "length" {
				status = "incomplete"
			}
		}
	}

	usage := map[string]interface{}{}
	if u, ok := chatResp["usage"].(map[string]interface{}); ok {
		inTok := numberToInt64(u["prompt_tokens"])
		outTok := numberToInt64(u["completion_tokens"])
		cached := int64(0)
		if details, ok := u["prompt_tokens_details"].(map[string]interface{}); ok {
			cached = numberToInt64(details["cached_tokens"])
		}
		usage = map[string]interface{}{
			"input_tokens":  inTok,
			"output_tokens": outTok,
			"total_tokens":  inTok + outTok,
			"input_tokens_details": map[string]interface{}{
				"cached_tokens": cached,
			},
			"output_tokens_details": map[string]interface{}{
				"reasoning_tokens": int64(0),
			},
		}
	}

	return map[string]interface{}{
		"id":          randomID("resp_"),
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      status,
		"model":       fullModelID,
		"output":      output,
		"output_text": outputText.String(),
		"usage":       usage,
	}
}
