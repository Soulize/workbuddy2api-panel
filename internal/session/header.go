package session

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
)

// clientSessionHeaderNames 是只用于网关内部粘性路由的客户端会话头候选。
// 顺序代表优先级：优先具体 harness，再回落通用会话头。
// 这些头绝不由本包写入上游请求；调用方只拿派生后的 key 做 sticky routing。
var clientSessionHeaderNames = []string{
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

const clientSessionKeyPrefix = "sid-"

// ExtractClientSessionKey 从常见 harness 的入站 HTTP header 中提取客户端 session ID，
// 并立即不可逆派生为内部粘性 key。原始 session ID 不返回、不持久化、不进入日志。
//
// 同一原始 session ID 即使经中间代理改了 header 名，也会得到同一个 sticky key；
// 这使 DSH/Claude Code/CPA 等链路可以共享会话身份，同时避免 header 名变化打断粘性。
// 未命中任何支持的 header 时返回空串，调用方应完整回落既有 body 粘性判断。
func ExtractClientSessionKey(h http.Header) string {
	for _, name := range clientSessionHeaderNames {
		if v := strings.TrimSpace(h.Get(name)); v != "" {
			return deriveClientSessionKey(v)
		}
	}
	return ""
}

// deriveClientSessionKey 只保留 SHA-256 摘要，避免 Redis sticky key 或其他内部状态
// 保存客户端原始 session ID。固定域分隔保证与其他派生 key 命名空间隔离且跨重启稳定。
func deriveClientSessionKey(sessionID string) string {
	sum := sha256.Sum256([]byte("workbuddy2api:client-session\x00" + sessionID))
	return clientSessionKeyPrefix + hex.EncodeToString(sum[:])
}
