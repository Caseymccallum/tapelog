package packs

import (
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

// filesystemAndSlackCases covers the pack that needed no condition repair
// (filesystem) and the pack whose rule order was wrong (slack).
var filesystemAndSlackCases = []caseSpec{
	{name: "env file", pack: "filesystem", tool: "read_file", args: `{"path":"/app/.env"}`, verdict: policy.VerdictDeny, rule: "no-secret-files", rationale: "secret material"},
	{name: "env variant", pack: "filesystem", tool: "read_file", args: `{"path":"/app/.env.production"}`, verdict: policy.VerdictDeny, rule: "no-secret-files", rationale: "secret material"},
	{name: "private key", pack: "filesystem", tool: "write_file", args: `{"path":"/keys/server.pem"}`, verdict: policy.VerdictDeny, rule: "no-secret-files", rationale: "secret material"},
	{name: "credential file", pack: "filesystem", tool: "read_file", args: `{"path":"/home/u/.aws/credentials"}`, verdict: policy.VerdictDeny, rule: "no-secret-files", rationale: "secret material"},
	{name: "ssh key", pack: "filesystem", tool: "read_file", args: `{"path":"/home/u/.ssh/id_rsa"}`, verdict: policy.VerdictDeny, rule: "no-ssh-and-hooks", rationale: "SSH keys are out of bounds"},
	{name: "git hook write", pack: "filesystem", tool: "write_file", args: `{"path":"/repo/.git/hooks/pre-commit"}`, verdict: policy.VerdictDeny, rule: "no-ssh-and-hooks", rationale: "supply-chain attack"},
	{name: "ordinary write", pack: "filesystem", tool: "write_file", args: `{"path":"/srv/app/config.yaml"}`, verdict: policy.VerdictConfirm, rule: "writes-need-confirm", rationale: "mutations need a human"},
	{name: "ordinary read", pack: "filesystem", tool: "read_file", args: `{"path":"/srv/app/README.md"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "reads are free"},
	// slack: the deny must precede the overlapping confirm
	{name: "send message", pack: "slack", tool: "send_message", args: `{"channel":"#ops","text":"hi"}`, verdict: policy.VerdictConfirm, rule: "sends-confirm", rationale: "messages to humans need a human"},
	{name: "create channel is denied", pack: "slack", tool: "create_channel", args: `{"name":"exfil"}`, verdict: policy.VerdictDeny, rule: "no-channel-creation", rationale: "the deny must not be shadowed by the confirm above it"},
	{name: "create conversation is denied", pack: "slack", tool: "create_conversation", args: `{"name":"exfil"}`, verdict: policy.VerdictDeny, rule: "no-channel-creation", rationale: "same"},
	{name: "archive channel is denied", pack: "slack", tool: "archive_channel", args: `{"channel":"#ops"}`, verdict: policy.VerdictDeny, rule: "no-channel-creation", rationale: "workspace structure is not the agent's"},
	{name: "other create is confirmed", pack: "slack", tool: "create_bookmark", args: `{"channel":"#ops"}`, verdict: policy.VerdictConfirm, rule: "sends-confirm", rationale: "the narrower deny must not swallow unrelated creates"},
	{name: "invite is confirmed", pack: "slack", tool: "invite_user", args: `{"channel":"#ops","user":"u"}`, verdict: policy.VerdictConfirm, rule: "sends-confirm", rationale: "adding a human to a channel is a send"},
	{name: "read channel", pack: "slack", tool: "read_channel", args: `{"channel":"#ops"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "reading is fine"},
}

func TestFilesystemAndSlackEnforce(t *testing.T) { runCases(t, filesystemAndSlackCases) }
