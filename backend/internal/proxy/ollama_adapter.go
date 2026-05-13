package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaAdapter wraps OpenAIAdapter but overrides FetchModels to use /api/tags (Ollama native spec).
type OllamaAdapter struct {
	OpenAIAdapter
}

func (a *OllamaAdapter) FetchModels(apiBaseURL string, apiKey string) ([]string, error) {
	// Try OpenAI-compat /v1/models first; fall back to native /api/tags.
	base := strings.TrimRight(apiBaseURL, "/")
	if strings.HasSuffix(base, "/v1") {
		base = strings.TrimSuffix(base, "/v1")
	}

	url := base + "/api/tags"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var modelIDs []string
	for _, m := range result.Models {
		name := m.Name
		if idx := strings.LastIndex(name, ":"); idx >= 0 {
			// keep full name including tag (e.g. "llama3:8b")
		}
		modelIDs = append(modelIDs, name)
	}
	return modelIDs, nil
}
