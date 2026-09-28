package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/session"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// TestClientSessionHeaderStickyOnly 验证客户端 session header 的安全边界：
//  1. header session 优先于 body 会话键做 sticky 选号；
//  2. 原始 session ID 不进入任何上游 header；
//  3. session header 不参与 X-Conversation-Request-ID 等上游会话头派生；
//  4. Router/Store 只保存摘要 key，不保存原始 session ID。
func TestClientSessionHeaderStickyOnly(t *testing.T) {
	st := newBindStore()
	sess := session.New(session.Config{
		TTL:       time.Minute,
		Store:     st,
		Available: func() []string { return []string{"u1", "u2"} },
	})
	p := testPoolWith(
		&auth.Auth{UID: "u1", AccessToken: "at-u1", ExpiresAt: 9999999999},
		&auth.Auth{UID: "u2", AccessToken: "at-u2", ExpiresAt: 9999999999},
	)

	var mu sync.Mutex
	var gotHeaders []http.Header
	up := &upstream.Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			gotHeaders = append(gotHeaders, r.Header.Clone())
			mu.Unlock()
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(sseOK)),
			}, nil
		})},
		ChatBaseCN:    "https://fake.example",
		BillingBaseCN: "https://fake.example",
	}
	h := NewHandler(Config{Pool: p, Upstream: up, Session: sess, SoftCooldown: time.Minute})

	const bodyConversation = "body-conversation"
	const rawA = "raw-dsh-session-secret-A"
	const rawB = "raw-dsh-session-secret-B"

	// body 粘性故意指向 u2；客户端 session header 粘性指向 u1。
	// 若实现错误地继续优先 body key，请求会走 u2，测试立即失败。
	sess.Bind(bodyConversation, "u2")
	headerA := make(http.Header)
	headerA.Set("X-DeepSeek-Harness-Session-Id", rawA)
	headerB := make(http.Header)
	headerB.Set("X-DeepSeek-Harness-Session-Id", rawB)
	keyA := session.ExtractClientSessionKey(headerA)
	keyB := session.ExtractClientSessionKey(headerB)
	if keyA == "" || keyB == "" || keyA == rawA || keyB == rawB {
		t.Fatal("client session ids must be converted to non-raw sticky keys")
	}
	sess.Bind(keyA, "u1")
	sess.Bind(keyB, "u1")

	body := `{"model":"glm-5.2","messages":[{"role":"user","content":"same turn"}],"metadata":{"conversation_id":"body-conversation"}}`
	for _, tc := range []struct {
		raw string
	}{
		{raw: rawA},
		{raw: rawB},
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("X-DeepSeek-Harness-Session-Id", tc.raw)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("session=%q code=%d body=%s", tc.raw, rec.Code, rec.Body)
		}
	}

	mu.Lock()
	headers := append([]http.Header(nil), gotHeaders...)
	mu.Unlock()
	if len(headers) != 2 {
		t.Fatalf("upstream calls=%d want 2", len(headers))
	}

	for i, hdr := range headers {
		if got := hdr.Get("Authorization"); got != "Bearer at-u1" {
			t.Fatalf("call %d authorization=%q want u1 (header sticky must override body sticky)", i, got)
		}
		if got := hdr.Get("X-DeepSeek-Harness-Session-Id"); got != "" {
			t.Fatalf("client harness header leaked upstream: %q", got)
		}
		for name, values := range hdr {
			for _, v := range values {
				if strings.Contains(v, rawA) || strings.Contains(v, rawB) {
					t.Fatalf("raw client session leaked upstream via %s=%q", name, v)
				}
			}
		}
	}

	// 两次请求只有客户端 session ID 不同，body 完全相同。上游轮级 ID 必须一致：
	// 证明 client session header 没有参与 ChatMeta / X-Conversation-Request-ID 派生。
	if a, b := headers[0].Get("X-Conversation-Request-ID"), headers[1].Get("X-Conversation-Request-ID"); a == "" || a != b {
		t.Fatalf("client session must not influence upstream conversation request id: %q vs %q", a, b)
	}
	// WorkBuddy 自己的 X-Session-ID 是账号级稳定指纹；允许存在，但绝不能等于客户端值。
	if a, b := headers[0].Get("X-Session-Id"), headers[1].Get("X-Session-Id"); a == "" || a != b || a == rawA || a == rawB {
		t.Fatalf("upstream X-Session-ID must remain account-derived, got %q / %q", a, b)
	}

	// body key 不应被 header-session 请求覆盖；只更新 header 摘要 key。
	if uid, ok := st.lastUID(bodyConversation); !ok || uid != "u2" {
		t.Fatalf("body sticky binding was unexpectedly modified: uid=%q ok=%v", uid, ok)
	}
	st.mu.Lock()
	for k := range st.binds {
		if strings.Contains(k, rawA) || strings.Contains(k, rawB) {
			st.mu.Unlock()
			t.Fatalf("raw client session id persisted in sticky store key: %q", k)
		}
	}
	st.mu.Unlock()
}
