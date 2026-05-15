package proxy

import (
	"omnirelay/internal/models"
	"omnirelay/internal/service"
)

type Adapter interface {
	BuildChatRequest(body map[string]interface{}) (string, map[string]interface{}, error)
	ParseChatResponse(body map[string]interface{}) (map[string]interface{}, error)
	BuildMessagesRequest(body map[string]interface{}) (string, map[string]interface{}, error)
	ParseMessagesResponse(body map[string]interface{}) (map[string]interface{}, error)
	// ParseStreamChunk transforms a raw SSE chunk and returns (transformed, inputTokens, outputTokens, err).
	ParseStreamChunk(data []byte) ([]byte, int64, int64, error)
	// ParseMessagesStreamChunk transforms a raw SSE chunk into Anthropic Messages SSE format.
	ParseMessagesStreamChunk(data []byte, state map[string]interface{}) ([]byte, int64, int64, error)
	FetchModels(apiBaseURL string, apiKey string) ([]string, error)
	SupportsEndpoint(path string) bool
	IsSameFormat() bool
}

type Engine struct {
	providerService *service.ProviderService
	modelService    *service.ModelService
	usageService    *service.UsageService
	adapters        map[string]Adapter
}

func NewEngine(ps *service.ProviderService, ms *service.ModelService, us *service.UsageService) *Engine {
	e := &Engine{
		providerService: ps,
		modelService:    ms,
		usageService:    us,
		adapters:        make(map[string]Adapter),
	}
	e.adapters["openai"] = &OpenAIAdapter{}
	e.adapters["lmstudio"] = &OpenAIAdapter{}
	e.adapters["ollama"] = &OllamaAdapter{}
	e.adapters["gemini"] = &GeminiAdapter{}
	e.adapters["anthropic"] = &AnthropicAdapter{}
	return e
}

func (e *Engine) getAdapter(providerType string) Adapter {
	return e.adapters[providerType]
}

func (e *Engine) resolveModel(fullModelID string, userID int64) (*models.Model, error) {
	return e.modelService.FindByFullID(fullModelID, userID)
}
