package proxy

import (
	"strings"

	"github.com/pkoukk/tiktoken-go"
)

// modelEncodingMap maps model name prefixes to tiktoken encoding names.
// The keys are matched as prefixes against the full model ID.
var modelEncodingMap = map[string]string{
	// OpenAI
	"o1":              "o200k_base",
	"o3":              "o200k_base",
	"gpt-4o":          "o200k_base",
	"gpt-4":           "cl100k_base",
	"gpt-3.5":         "cl100k_base",
	"text-embedding":  "cl100k_base",
	"text-davinci-003": "p50k_base",
	"text-davinci-002": "p50k_base",
	"code-davinci":    "p50k_base",

	// Anthropic / Gemini / etc. lack public encoder registrations,
	// so they fall through to heuristic below.  We list them only so
	// the model-to-encoding lookup returns "" cleanly.
}

// countTextTokens attempts to count tokens via tiktoken and falls
// back to a character-based heuristic when the encoding is unknown.
func countTextTokens(text, fullModelID string) int64 {
	if text == "" {
		return 0
	}

	modelID := stripProviderPrefix(fullModelID)
	if enc := encodingForModel(modelID); enc != "" {
		if tk, err := tiktoken.GetEncoding(enc); err == nil {
			tokens := tk.EncodeOrdinary(text)
			return int64(len(tokens))
		}
	}
	// Also try EncodingForModel as a backup.
	if tk, err := tiktoken.EncodingForModel(modelID); err == nil {
		tokens := tk.EncodeOrdinary(text)
		return int64(len(tokens))
	}

	return heuristicTokenCount(text)
}

// encodingForModel returns the tiktoken encoding name for the model,
// matching by exact name and then by prefix.
func encodingForModel(modelID string) string {
	if enc, ok := modelEncodingMap[modelID]; ok {
		return enc
	}
	for prefix, enc := range modelEncodingMap {
		if strings.HasPrefix(modelID, prefix) {
			return enc
		}
	}
	return ""
}

// heuristicTokenCount estimates tokens using character-class weighting.
//   - CJK / Hangul: ~1.5 chars per token
//   - Latin letters: ~4 chars per token
//   - Digits:        ~3 chars per token
//   - Whitespace:    negligible (merged with adjacent tokens)
//   - Other:         ~2 chars per token
func heuristicTokenCount(text string) int64 {
	if text == "" {
		return 0
	}

	var cjkCount, latinCount, digitCount, otherCount int
	for _, r := range text {
		switch {
		case isCJK(r):
			cjkCount++
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			latinCount++
		case r >= '0' && r <= '9':
			digitCount++
		case r == ' ' || r == '\n' || r == '\t' || r == '\r':
			// whitespace – skipped
		default:
			otherCount++
		}
	}

	total := float64(cjkCount)/1.5 +
		float64(latinCount)/4.0 +
		float64(digitCount)/3.0 +
		float64(otherCount)/2.0

	if total < 1 {
		return 1
	}
	return int64(total + 0.5)
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || // CJK Unified Ideographs
		(r >= 0xAC00 && r <= 0xD7AF) || // Hangul Syllables
		(r >= 0x3040 && r <= 0x30FF) || // Hiragana + Katakana
		(r >= 0x3400 && r <= 0x4DBF) || // CJK Extension A
		(r >= 0x20000 && r <= 0x2A6DF) || // CJK Extension B
		(r >= 0x2E80 && r <= 0x2EFF) || // CJK Radicals
		(r >= 0x3000 && r <= 0x303F) // CJK Symbols & Punctuation
}

// extractInputText pulls all textual content from a request body
// regardless of API format (OpenAI, Anthropic, Gemini).
func extractInputText(body map[string]interface{}) string {
	if body == nil {
		return ""
	}

	var texts []string

	// 1. Messages array (OpenAI / Anthropic format)
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, raw := range msgs {
			if msg, ok := raw.(map[string]interface{}); ok {
				texts = append(texts, extractContentText(msg["content"]))
			}
		}
	}

	// 2. System prompt (Anthropic / OpenAI)
	if sys, ok := body["system"].(string); ok && sys != "" {
		texts = append(texts, sys)
	}

	// 3. Gemini-style contents array
	if contents, ok := body["contents"].([]interface{}); ok {
		for _, raw := range contents {
			if c, ok := raw.(map[string]interface{}); ok {
				if parts, ok := c["parts"].([]interface{}); ok {
					for _, p := range parts {
						if part, ok := p.(map[string]interface{}); ok {
							if t, ok := part["text"].(string); ok {
								texts = append(texts, t)
							}
						}
					}
				}
			}
		}
	}

	return strings.Join(texts, " ")
}

// extractContentText extracts text from a message's content field,
// which can be a plain string or an array of content blocks.
func extractContentText(content interface{}) string {
	switch c := content.(type) {
	case string:
		return c
	case []interface{}:
		var blocks []string
		for _, raw := range c {
			if block, ok := raw.(map[string]interface{}); ok {
				if block["type"] == "text" {
					if t, ok := block["text"].(string); ok {
						blocks = append(blocks, t)
					}
				}
			}
		}
		return strings.Join(blocks, " ")
	}
	return ""
}

// extractOutputText pulls the assistant response text from a response body
// based on the provider type.
func extractOutputText(resp map[string]interface{}, providerType string) string {
	if resp == nil {
		return ""
	}

	switch providerType {
	case "anthropic":
		if content, ok := resp["content"].([]interface{}); ok {
			for _, raw := range content {
				if block, ok := raw.(map[string]interface{}); ok {
					if block["type"] == "text" {
						if t, ok := block["text"].(string); ok {
							return t
						}
					}
				}
			}
		}
		return ""

	case "gemini":
		if candidates, ok := resp["candidates"].([]interface{}); ok {
			for _, raw := range candidates {
				if c, ok := raw.(map[string]interface{}); ok {
					if content, ok := c["content"].(map[string]interface{}); ok {
						if parts, ok := content["parts"].([]interface{}); ok {
							for _, p := range parts {
								if part, ok := p.(map[string]interface{}); ok {
									if t, ok := part["text"].(string); ok {
										return t
									}
								}
							}
						}
					}
				}
			}
		}
		return ""

	default: // openai, lmstudio, ollama
		if choices, ok := resp["choices"].([]interface{}); ok {
			for _, raw := range choices {
				if choice, ok := raw.(map[string]interface{}); ok {
					if msg, ok := choice["message"].(map[string]interface{}); ok {
						if content, ok := msg["content"].(string); ok {
							return content
						}
					}
				}
			}
		}
		return ""
	}
}

// countInputTokens counts input tokens from the original request body.
func countInputTokens(body map[string]interface{}, fullModelID string) int64 {
	text := extractInputText(body)
	return countTextTokens(text, fullModelID)
}

// countOutputTokens counts output tokens from the response body.
func countOutputTokens(resp map[string]interface{}, providerType, fullModelID string) int64 {
	text := extractOutputText(resp, providerType)
	return countTextTokens(text, fullModelID)
}
