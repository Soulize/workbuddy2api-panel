package session

import (
	"net/http"
	"strings"
	"testing"
)

func TestExtractClientSessionKeySupportedHeaders(t *testing.T) {
	headers := []string{
		"X-DeepSeek-Harness-Session-Id",
		"X-Claude-Code-Session-Id",
		"X-Codex-Session-Id",
		"X-OpenCode-Session-Id",
		"X-CPA-Session-Id",
		"X-Cursor-Session-Id",
		"X-Antigravity-Session-Id",
		"X-Gemini-Session-Id",
		"X-Kiro-Session-Id",
		"X-OpenAI-Session-Id",
		"X-Session-Id",
		"Session-Id",
	}
	const raw = "super-secret-client-session-123"
	want := deriveClientSessionKey(raw)

	for _, name := range headers {
		t.Run(name, func(t *testing.T) {
			h := make(http.Header)
			h.Set(name, raw)
			got := ExtractClientSessionKey(h)
			if got != want {
				t.Fatalf("key=%q want %q", got, want)
			}
			if strings.Contains(got, raw) {
				t.Fatalf("derived key must not contain raw session id: %q", got)
			}
			if !strings.HasPrefix(got, clientSessionKeyPrefix) {
				t.Fatalf("key=%q missing prefix %q", got, clientSessionKeyPrefix)
			}
		})
	}
}

func TestExtractClientSessionKeyPriority(t *testing.T) {
	h := make(http.Header)
	h.Set("X-DeepSeek-Harness-Session-Id", "dsh-session")
	h.Set("X-Session-Id", "generic-session")
	got := ExtractClientSessionKey(h)
	want := deriveClientSessionKey("dsh-session")
	if got != want {
		t.Fatalf("specific harness header must win: got %q want %q", got, want)
	}
}

func TestExtractClientSessionKeySameIDAcrossHeaderNames(t *testing.T) {
	h1 := make(http.Header)
	h1.Set("X-Claude-Code-Session-Id", "same-session")
	h2 := make(http.Header)
	h2.Set("X-CPA-Session-Id", "same-session")
	if a, b := ExtractClientSessionKey(h1), ExtractClientSessionKey(h2); a != b {
		t.Fatalf("same session id via different proxy headers must keep sticky identity: %q != %q", a, b)
	}
}

func TestExtractClientSessionKeyEmpty(t *testing.T) {
	if got := ExtractClientSessionKey(nil); got != "" {
		t.Fatalf("nil header => %q want empty", got)
	}
	h := make(http.Header)
	h.Set("X-DeepSeek-Harness-Session-Id", "   ")
	if got := ExtractClientSessionKey(h); got != "" {
		t.Fatalf("blank session id => %q want empty", got)
	}
}
