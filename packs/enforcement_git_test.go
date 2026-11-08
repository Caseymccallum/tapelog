package packs

import (
	"testing"

	"github.com/Caseymccallum/tapelog/internal/policy"
)

// gitCases covers history destruction, hook tampering and the push confirm.
var gitCases = []caseSpec{
	{name: "force push", pack: "git", tool: "run_git_command", args: `{"command":"git push --force origin main"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "the M5 evidence scenario: previously allowed"},
	{name: "force push short flag", pack: "git", tool: "run_git_command", args: `{"command":"git push -f origin main"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "same operation, short flag"},
	{name: "force push trailing flag", pack: "git", tool: "run_git_command", args: `{"command":"git push origin main --force"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "flag after the refspec"},
	{name: "quoted force push", pack: "git", tool: "run_git_command", args: `{"command":"git push \"--force\" origin main"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "shell quoting does not stop git honouring the flag"},
	{name: "single quoted short flag", pack: "git", tool: "run_git_command", args: `{"command":"git push '-f' origin main"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "same, single quotes"},
	{name: "force with lease", pack: "git", tool: "run_git_command", args: `{"command":"git push --force-with-lease origin main"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "still rewrites shared history"},
	{name: "reset hard", pack: "git", tool: "run_git_command", args: `{"command":"git reset --hard HEAD~3"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "discards work"},
	{name: "filter-branch", pack: "git", tool: "run_git_command", args: `{"command":"git filter-branch --tree-filter x"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "rewrites history"},
	{name: "filter-repo", pack: "git", tool: "run_git_command", args: `{"command":"git filter-repo --path x"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "rewrites history"},
	{name: "clean xdf", pack: "git", tool: "run_git_command", args: `{"command":"git clean -xdf"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "deletes untracked work"},
	{name: "clean fd", pack: "git", tool: "run_git_command", args: `{"command":"git clean -fd"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "deletes untracked work"},
	{name: "update-ref delete", pack: "git", tool: "run_git_command", args: `{"command":"git update-ref -d refs/heads/main"}`, verdict: policy.VerdictDeny, rule: "no-history-destruction", rationale: "deletes a ref"},
	{name: "hooksPath tamper", pack: "git", tool: "run_git_command", args: `{"command":"git config core.hooksPath /tmp/h"}`, verdict: policy.VerdictDeny, rule: "no-hook-or-config-tamper", rationale: "supply-chain attack"},
	{name: "upload-pack tamper", pack: "git", tool: "run_git_command", args: `{"command":"git clone --upload-pack=/tmp/x origin"}`, verdict: policy.VerdictDeny, rule: "no-hook-or-config-tamper", rationale: "supply-chain attack"},
	{name: "receive-pack tamper", pack: "git", tool: "run_git_command", args: `{"command":"git push --receive-pack=/tmp/x origin"}`, verdict: policy.VerdictDeny, rule: "no-hook-or-config-tamper", rationale: "supply-chain attack"},
	// the same rules apply through the narrower git tool namespace
	{name: "force push via git_ tool", pack: "git", tool: "git_push", args: `{"command":"--force origin main"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "documented gap: the pattern needs the word push, which this argument shape omits"},
	// legitimate operations
	{name: "ordinary push", pack: "git", tool: "run_git_command", args: `{"command":"git push origin main"}`, verdict: policy.VerdictConfirm, rule: "pushes-confirm", rationale: "pushes change shared state"},
	{name: "new branch push", pack: "git", tool: "run_git_command", args: `{"command":"git push -u origin feature/x"}`, verdict: policy.VerdictConfirm, rule: "pushes-confirm", rationale: "same"},
	{name: "status", pack: "git", tool: "run_git_command", args: `{"command":"git status"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "reads are free"},
	{name: "log", pack: "git", tool: "run_git_command", args: `{"command":"git log --oneline -5"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "reads are free"},
	{name: "safe fetch", pack: "git", tool: "run_git_command", args: `{"command":"git fetch origin"}`, verdict: policy.VerdictAllow, rule: "default", rationale: "does not change shared state"},
}

func TestGitDeniesEnforce(t *testing.T) { runCases(t, gitCases) }
