package proxy

// boolState safely reads a boolean from the shared streaming state map.
func boolState(state map[string]interface{}, key string) bool {
	v, _ := state[key].(bool)
	return v
}

// int64State safely reads an int64 from the shared streaming state map, returning fallback if absent.
func int64State(state map[string]interface{}, key string, fallback int64) int64 {
	if v, ok := state[key].(int64); ok {
		return v
	}
	return fallback
}
