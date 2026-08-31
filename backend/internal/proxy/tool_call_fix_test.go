package proxy

import (
	"strings"
	"testing"
)

// Exact shape from error.txt: prose, then multiple <tool_call> tags whose JSON
// is line-wrapped mid-string and contains unescaped inner quotes.
func TestParseToolCallTagsFromLeakedLog(t *testing.T) {
	content := "Starting a standard security scan. Let me load the required scan references.\n" +
		"<tool_call>\n" +
		"{\"name\": \"exec\", \"arguments\": {\"cmd\": \"echo \"CODEX_SECURITY_SCAN_ID=${CODEX_SECURITY_SCAN_ID:-unset}\" && echo\n" +
		"  \"CODEX_SECURITY_SCAN_DIR=${CODEX_SECURITY_SCAN_DIR:-unset}\"\"}}\n" +
		"</tool_call>\n" +
		"<tool_call>\n" +
		"{\"name\": \"exec\", \"arguments\": {\"cmd\": \"ls /home/nergis/.codex/plugins/cache/openai-api-curated/codex-\n" +
		"security/1e285826/references/\"}}\n" +
		"</tool_call>\n" +
		"<tool_call>\n" +
		"{\"name\": \"exec\", \"arguments\": {\"cmd\": \"cat /home/nergis/.codex/plugins/cache/openai-api-curated/codex-\n" +
		"security/1e285826/references/scan-prologue.md\"}}\n" +
		"</tool_call>"

	tcs, ok := parseContentAsToolCalls(content, nil)
	if !ok {
		t.Fatal("expected leaked <tool_call> log to parse")
	}
	if len(tcs) != 3 {
		t.Fatalf("got %d tool calls, want 3", len(tcs))
	}
	for i, tc := range tcs {
		fn, _ := tc["function"].(map[string]interface{})
		if fn["name"] != "exec" {
			t.Errorf("tcs[%d].name = %v, want exec", i, fn["name"])
		}
		if id, _ := tc["id"].(string); id == "" {
			t.Errorf("tcs[%d].id missing", i)
		}
	}
	args, _ := tcs[1]["function"].(map[string]interface{})["arguments"].(string)
	if !strings.Contains(args, "codex-security/1e285826") {
		t.Errorf("wrapped string not rejoined: %q", args)
	}
}

func TestParseToolCallTagUnterminated(t *testing.T) {
	tcs, ok := parseContentAsToolCalls("prefix text <tool_call>\n{\"name\": \"exec\", \"arguments\": {}}", nil)
	if !ok || len(tcs) != 1 {
		t.Fatalf("unterminated tag: ok=%v tcs=%v", ok, tcs)
	}
}

func TestParsePlainJSONStillWorks(t *testing.T) {
	valid := map[string]bool{"get_weather": true}
	tcs, ok := parseContentAsToolCalls(`{"name":"get_weather","arguments":{"city":"Seoul"}}`, valid)
	if !ok || len(tcs) != 1 {
		t.Fatalf("plain JSON: ok=%v tcs=%v", ok, tcs)
	}
	if _, ok := parseContentAsToolCalls(`{"city":"Seoul"}`, valid); ok {
		t.Error("JSON without a tool name must not convert")
	}
	if _, ok := parseContentAsToolCalls(`{"name":"unknown_tool","arguments":{}}`, valid); ok {
		t.Error("unknown tool name must be rejected in non-tag path")
	}
}
