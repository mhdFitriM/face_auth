package main

import "testing"

func TestNormalizeCloudHTTPBase(t *testing.T) {
	tests := map[string]string{
		"ws://example.test/agent/ws":   "http://example.test",
		"wss://example.test/agent/ws/": "https://example.test",
		"http://example.test:8082/":    "http://example.test:8082",
		"https://example.test":         "https://example.test",
	}
	for input, want := range tests {
		if got := normalizeCloudHTTPBase(input); got != want {
			t.Errorf("normalizeCloudHTTPBase(%q) = %q, want %q", input, got, want)
		}
	}
}
