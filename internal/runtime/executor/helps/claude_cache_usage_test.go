package helps

import "testing"

func TestClaudeCacheCreationTTLParsing(t *testing.T) {
	for _, tc := range []struct {
		name, payload     string
		total, five, hour int64
	}{
		{"mixed", `{"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":70}}`, 100, 30, 70},
		{"one hour", `{"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_1h_input_tokens":100}}`, 100, 0, 100},
		{"details only", `{"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":70}}`, 100, 30, 70},
		{"missing TTL", `{"cache_creation_input_tokens":100}`, 100, 0, 0},
		{"partial", `{"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_1h_input_tokens":40}}`, 100, 60, 40},
		{"over reported", `{"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":60,"ephemeral_1h_input_tokens":140}}`, 100, 30, 70},
		{"explicit zero total", `{"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_1h_input_tokens":100}}`, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ParseClaudeUsage([]byte(`{"usage":` + tc.payload + `}`))
			if d.CacheCreationTokens != tc.total || d.CacheCreation5mTokens != tc.five || d.CacheCreation1hTokens != tc.hour || d.TotalTokens != tc.total || !d.TokenBreakdown.Valid() {
				t.Fatalf("unexpected detail: %+v", d)
			}
		})
	}
}

func TestClaudeStreamPreservesCacheTTLUntilFinalUsage(t *testing.T) {
	for _, last := range []string{
		`{"output_tokens":12}`,
		`{"output_tokens":12,"cache_creation_input_tokens":100}`,
	} {
		var buffer StreamUsageBuffer
		for _, line := range []string{
			`data: {"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":20,"cache_creation_input_tokens":100,"cache_creation":{"ephemeral_5m_input_tokens":30,"ephemeral_1h_input_tokens":70}}}}`,
			`data: {"type":"message_delta","usage":` + last + `}`,
		} {
			d, ok := ParseClaudeStreamUsage([]byte(line))
			if !ok {
				t.Fatal("missing stream usage")
			}
			ObserveMergedStreamUsage(&buffer, d)
		}
		d, _ := buffer.Detail()
		if d.InputTokens != 10 || d.OutputTokens != 12 || d.CacheCreation5mTokens != 30 || d.CacheCreation1hTokens != 70 || d.TotalTokens != 142 || !d.TokenBreakdown.Valid() {
			t.Fatalf("lost stream buckets or duplicated tokens: %+v", d)
		}
	}
}
