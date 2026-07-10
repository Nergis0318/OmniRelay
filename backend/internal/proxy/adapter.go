package proxy

import (
	"net/http"
	"omnirelay/internal/hub"
	"omnirelay/internal/models"
	"omnirelay/internal/service"
)

type Adapter interface {
	BuildChatRequest(body map[string]interface{}) (string, map[string]interface{}, error)
	ParseChatResponse(body map[string]interface{}) (map[string]interface{}, error)
	BuildMessagesRequest(body map[string]interface{}) (string, map[string]interface{}, error)
	ParseMessagesResponse(body map[string]interface{}) (map[string]interface{}, error)
	// ParseStreamChunk transforms a raw SSE chunk and returns (transformed, inputTokens, outputTokens, err).
	// The state map persists across chunks to carry provider-specific values (e.g., input_tokens from message_start).
	ParseStreamChunk(data []byte, state map[string]interface{}) ([]byte, int64, int64, error)
	// ParseMessagesStreamChunk transforms a raw SSE chunk into Anthropic Messages SSE format.
	ParseMessagesStreamChunk(data []byte, state map[string]interface{}) ([]byte, int64, int64, error)
}

type Engine struct {
	providerService *service.ProviderService
	modelService    *service.ModelService
	usageService    *service.UsageService
	httpClient      *http.Client
	adapters        map[string]Adapter
	hub             *hub.Hub
}

func NewEngine(ps *service.ProviderService, ms *service.ModelService, us *service.UsageService, h *hub.Hub) *Engine {
	e := &Engine{
		providerService: ps,
		modelService:    ms,
		usageService:    us,
		httpClient:      &http.Client{Timeout: upstreamRequestTimeout},
		adapters:        make(map[string]Adapter),
		hub:             h,
	}
	e.adapters["openai"] = &OpenAIAdapter{}
	e.adapters["lmstudio"] = &OpenAIAdapter{}
	e.adapters["ollama"] = &OpenAIAdapter{}
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

func (e *Engine) TestProvider(provider *models.Provider, apiKey, modelID string) TestProviderResult {
	return TestProvider(provider, apiKey, modelID, e.adapters, e.httpClient)
}
