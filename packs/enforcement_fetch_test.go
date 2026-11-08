package packs

import (
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

// fetchCases covers SSRF and the TLS-for-posts rule.
var fetchCases = []caseSpec{
	{name: "localhost", pack: "fetch", tool: "fetch", args: `{"url":"http://localhost:8080/x"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "SSRF pivot"},
	{name: "loopback", pack: "fetch", tool: "fetch", args: `{"url":"http://127.0.0.1/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "SSRF pivot"},
	{name: "ipv6 loopback", pack: "fetch", tool: "fetch", args: `{"url":"http://[::1]/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "SSRF pivot"},
	{name: "rfc1918 ten", pack: "fetch", tool: "fetch", args: `{"url":"http://10.1.2.3/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "SSRF pivot"},
	{name: "rfc1918 192", pack: "fetch", tool: "fetch", args: `{"url":"http://192.168.1.1/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "SSRF pivot"},
	{name: "rfc1918 172.16", pack: "fetch", tool: "fetch", args: `{"url":"http://172.16.0.1/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "the whole 172.16/12 range, not a character class"},
	{name: "rfc1918 172.20", pack: "fetch", tool: "fetch", args: `{"url":"http://172.20.0.1/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "middle of the range"},
	{name: "rfc1918 172.31", pack: "fetch", tool: "fetch", args: `{"url":"http://172.31.255.255/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "top of the range"},
	{name: "cloud metadata", pack: "fetch", tool: "fetch", args: `{"url":"http://169.254.169.254/latest/meta-data/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "credential theft"},
	{name: "metadata hostname", pack: "fetch", tool: "fetch", args: `{"url":"http://metadata.google.internal/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "credential theft"},
	{name: "cgnat", pack: "fetch", tool: "fetch", args: `{"url":"http://100.64.0.1/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "carrier-grade NAT space"},
	{name: "https loopback", pack: "fetch", tool: "fetch", args: `{"url":"https://localhost/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "the scheme is not a bypass"},
	{name: "https metadata", pack: "fetch", tool: "fetch", args: `{"url":"https://169.254.169.254/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "the scheme is not a bypass"},
	{name: "ftp loopback", pack: "fetch", tool: "fetch", args: `{"url":"ftp://127.0.0.1/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "any scheme"},
	{name: "uppercase scheme metadata", pack: "fetch", tool: "fetch", args: `{"url":"HTTPS://169.254.169.254/"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "scheme casing is not a bypass for a numeric host"},
	{name: "download tool", pack: "fetch", tool: "download_file", args: `{"url":"http://10.0.0.5/secret"}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "every tool glob in the rule is covered"},
	{name: "extra arguments", pack: "fetch", tool: "fetch", args: `{"url":"http://10.0.0.5/","headers":{"accept":"x"},"timeout":5}`, verdict: policy.VerdictDeny, rule: "no-internal-urls", rationale: "a richer argument shape does not change the target"},
	// legitimate operations
	{name: "public https", pack: "fetch", tool: "fetch", args: `{"url":"https://example.com/"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "public URLs are fine"},
	{name: "public 172.32", pack: "fetch", tool: "fetch", args: `{"url":"http://172.32.0.1/"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "172.32 is outside 172.16/12"},
	{name: "tls post", pack: "fetch", tool: "post", args: `{"url":"https://example.com/x","body":"hi"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "outbound data over TLS"},
	{name: "uppercase tls post", pack: "fetch", tool: "post", args: `{"url":"HTTPS://example.com/x","body":"hi"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "the client normalises the scheme"},
	// the rule must fail closed
	{name: "plaintext post", pack: "fetch", tool: "post", args: `{"url":"http://example.com/x"}`, verdict: policy.VerdictDeny, rule: "https-only-for-posts", rationale: "outbound data must travel over TLS"},
	{name: "post without a url", pack: "fetch", tool: "post", args: `{"body":"no url argument"}`, verdict: policy.VerdictDeny, rule: "https-only-for-posts", rationale: "no url cannot prove TLS, so it must not fall through to allow"},
	{name: "post with a nested url", pack: "fetch", tool: "post", args: `{"request":{"url":"http://x"}}`, verdict: policy.VerdictDeny, rule: "https-only-for-posts", rationale: "a nested url is not the documented argument, so it fails closed"},
}

func TestFetchDeniesEnforce(t *testing.T) { runCases(t, fetchCases) }
