package passthrough

import "testing"

func TestExtractModel(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "plain json", body: `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`, want: "gpt-4o"},
		{name: "empty object", body: `{}`, want: ""},
		{name: "no model field", body: `{"messages":[]}`, want: ""},
		{name: "empty body", body: ``, want: ""},
		{name: "whitespace", body: `   `, want: ""},
		{name: "model not a string", body: `{"model":123}`, want: ""},
		{name: "sse data line", body: "data: {\"model\":\"claude-3-5-sonnet\"}\n\ndata: [DONE]\n\n", want: "claude-3-5-sonnet"},
		{name: "newline delimited", body: "{\"model\":\"llama3\"}\n{\"done\":true}", want: "llama3"},
		{name: "gpt-4o-mini", body: `{"model":"gpt-4o-mini"}`, want: "gpt-4o-mini"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractModel([]byte(tc.body)); got != tc.want {
				t.Errorf("extractModel(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}
