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
	ParseStreamChunk(data []byte) ([]byte, error)
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
	e.adapters["ollama"] = &OpenAIAdapter{}
	e.adapters["gemini"] = &GeminiAdapter{}
	e.adapters["anthropic"] = &AnthropicAdapter{}
	return e
}

func (e *Engine) getAdapter(providerType string) Adapter {
	return e.adapters[providerType]
}

func (e *Engine) resolveModel(fullModelID string) (*models.Model, error) {
	return e.modelService.FindByFullID(fullModelID)
}
