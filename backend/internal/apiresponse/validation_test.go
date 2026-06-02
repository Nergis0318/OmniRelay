package apiresponse

import "testing"

func TestValidateChatCompletionBody(t *testing.T) {
	tests := []struct {
		name    string
		body    map[string]interface{}
		wantPar string
		wantErr bool
	}{
		{
			name: "valid",
			body: map[string]interface{}{
				"model":    "gpt-4o",
				"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
			},
		},
		{
			name:    "missing model",
			body:    map[string]interface{}{"messages": []interface{}{}},
			wantPar: "model",
			wantErr: true,
		},
		{
			name:    "missing messages",
			body:    map[string]interface{}{"model": "gpt-4o"},
			wantPar: "messages",
			wantErr: true,
		},
		{
			name:    "empty messages",
			body:    map[string]interface{}{"model": "gpt-4o", "messages": []interface{}{}},
			wantPar: "messages",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			param, err := ValidateChatCompletionBody(tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if param != tt.wantPar {
					t.Errorf("param = %q, want %q", param, tt.wantPar)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateMessagesBody(t *testing.T) {
	tests := []struct {
		name    string
		body    map[string]interface{}
		wantPar string
		wantErr bool
	}{
		{
			name: "valid",
			body: map[string]interface{}{
				"model":      "claude-sonnet-4-6",
				"max_tokens": 1024,
				"messages":   []interface{}{map[string]interface{}{"role": "user", "content": "Hello"}},
			},
		},
		{
			name:    "missing max_tokens",
			body:    map[string]interface{}{"model": "claude", "messages": []interface{}{map[string]interface{}{}}},
			wantPar: "max_tokens",
			wantErr: true,
		},
		{
			name: "max_tokens float",
			body: map[string]interface{}{
				"model":      "claude",
				"max_tokens": float64(512),
				"messages":   []interface{}{map[string]interface{}{}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			param, err := ValidateMessagesBody(tt.body)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if param != tt.wantPar {
					t.Errorf("param = %q, want %q", param, tt.wantPar)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}