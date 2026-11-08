package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Caseymccallum/tapelog/internal/session"
	"github.com/Caseymccallum/tapelog/internal/tui"
)

func setupRun(args ...string) (string, error) {
	cmd := newSetupCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestSetupPreservesConfigAndNeverOverwrites(t *testing.T) {
	for _, client := range []string{"roo", "cline", "vscode", "claude-code", "claude-desktop"} {
		t.Run(client, func(t *testing.T) {
			dir := t.TempDir()
			config := filepath.Join(dir, "client.json")
			output := filepath.Join(dir, "candidate.json")
			key := "mcpServers"
			if client == "vscode" {
				key = "servers"
			}
			original := `{"` + key + `":{"chosen":{"command":"node","args":["server with spaces.js","secret-in-argv"],"env":{"TOKEN":"private-value"},"cwd":"original cwd","alwaysAllow":["read"]},"other":{"command":"untouched"}},"inputs":[{"id":"prompt"}],"future":{"n":9007199254740993}}`
			if err := os.WriteFile(config, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"--client", client, "--config", config, "--server", "chosen", "--log", filepath.Join(dir, "new log.jsonl"), "--output", output}
			out, err := setupRun(append(args, "--dry-run")...)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("dry run created output")
			}
			for _, secret := range []string{"private-value", "secret-in-argv"} {
				if strings.Contains(out, secret) {
					t.Fatal("preview exposed credentials")
				}
			}
			out, err = setupRun(args...)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out, "private-value") || strings.Contains(out, "secret-in-argv") {
				t.Fatal("save exposed credentials")
			}
			got, _ := os.ReadFile(config)
			if string(got) != original {
				t.Fatal("source modified")
			}
			candidate, _ := os.ReadFile(output)
			var doc map[string]json.RawMessage
			_ = json.Unmarshal(candidate, &doc)
			var servers map[string]json.RawMessage
			_ = json.Unmarshal(doc[key], &servers)
			var entry struct {
				Command     string
				Args        []string
				Env         map[string]string
				Cwd         string
				AlwaysAllow []string
			}
			_ = json.Unmarshal(servers["chosen"], &entry)
			if !filepath.IsAbs(entry.Command) || entry.Env["TOKEN"] != "private-value" || entry.Cwd != "original cwd" || len(entry.AlwaysAllow) != 1 {
				t.Fatalf("lost entry settings: %s", candidate)
			}
			if entry.Args[len(entry.Args)-1] != "secret-in-argv" || strings.Contains(strings.Join(entry.Args, " "), "--auto-confirm") {
				t.Fatal("changed argv/confirm")
			}
			if !strings.Contains(string(doc["future"]), "9007199254740993") || !strings.Contains(string(servers["other"]), "untouched") || len(doc["inputs"]) == 0 {
				t.Fatal("lost other settings")
			}
			if _, err := setupRun(args...); err == nil {
				t.Fatal("overwrote existing candidate")
			}
			if _, err := setupRun("--client", client, "--config", config, "--server", "chosen", "--output", config, "--log", filepath.Join(dir, "other.jsonl")); err == nil {
				t.Fatal("overwrote source")
			}
			got, _ = os.ReadFile(config)
			if string(got) != original {
				t.Fatal("source changed after refusal")
			}
		})
	}
}

func TestSetupRejectsInvalidInputs(t *testing.T) {
	for _, raw := range []string{
		`{"mcpServers":`, `{"mcpServers":{},"mcpServers":{}}`,
		`{"mcpServers":{"chosen":{"command":"node","env":{"A":"1","A":"2"}}}}`,
		`{"mcpServers":{"chosen":{"url":"https://invalid.example"}}}`,
		`{"mcpServers":{"chosen":{"command":"node","type":"http"}}}`,
		`{"mcpServers":{"chosen":{"command":"tapelog","args":["record"]}}}`,
		`{"mcpServers":{"chosen":{"command":"node","args":[null]}}}`,
		`{"mcpServers":{"chosen":{"command":"node","args":null}}}`,
		`{"mcpServers":{"chosen":{"command":"node","args":[1]}}}`,
		`{"mcpServers":{}}`, `null`, `{} {}`,
	} {
		dir := t.TempDir()
		config := filepath.Join(dir, "c.json")
		output := filepath.Join(dir, "out.json")
		_ = os.WriteFile(config, []byte(raw), 0600)
		if _, err := setupRun("--client", "roo", "--config", config, "--server", "chosen", "--output", output, "--log", filepath.Join(dir, "new.jsonl")); err == nil {
			t.Fatalf("accepted %s", raw)
		}
		got, _ := os.ReadFile(config)
		if string(got) != raw {
			t.Fatal("invalid source changed")
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid setup created output")
		}
	}
}

func TestDoctorExecutableLogAndPort(t *testing.T) {
	dir := t.TempDir()
	exe, _ := os.Executable()
	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--server", exe, "--log", filepath.Join(dir, "extensionless")})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("extensionless file: %v; %s", err, out.String())
	}
	cmd = newDoctorCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--server", "tapelog-nonexistent-test-executable"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("missing executable passed")
	}
	cmd = newDoctorCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--log", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatal("directory as log passed")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := checkListen(listener.Addr().String()); err == nil {
		t.Fatal("occupied port passed")
	}
	for _, address := range []string{"0.0.0.0:8923", "example.com:8923", "127.0.0.1:0", "127.0.0.1:invalid"} {
		if checkListen(address) == nil {
			t.Fatalf("accepted %s", address)
		}
	}
}

// Exercise the actual CLI and a real subprocess over stdio, with each request
// waiting for its response. Closing stdin early can otherwise lose fixture replies.
func talk(t *testing.T, binary string, args []string, requests []string) []json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 65536), 1024*1024)
	var responses []json.RawMessage
	for _, request := range requests {
		if _, err := in.Write([]byte(request + "\n")); err != nil {
			t.Fatal(err)
		}
		if !scan.Scan() {
			t.Fatalf("missing response: %s; %v", stderr.String(), scan.Err())
		}
		responses = append(responses, append(json.RawMessage(nil), scan.Bytes()...))
	}
	_ = in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("CLI failed: %v; %s", err, stderr.String())
	}
	return responses
}

func TestDXFirstSessionPathsWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Projects", "Tapelog")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "tapelog"+suffix)
	server := filepath.Join(dir, "fixture server"+suffix)
	for _, build := range []struct{ out, pkg string }{{binary, "."}, {server, "../../internal/compat/fakecmd"}} {
		cmd := exec.Command("go", "build", "-o", build.out, build.pkg)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
	}
	config := filepath.Join(dir, "client.json")
	candidate := filepath.Join(dir, "candidate.json")
	logPath := filepath.Join(dir, "first recording.jsonl")
	raw, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fixture": map[string]any{"command": server, "args": []string{"-era", "2026-07-28"}}}})
	_ = os.WriteFile(config, raw, 0600)
	policyPath := filepath.Join(dir, "deny.yaml")
	_ = os.WriteFile(policyPath, []byte("version: 1\ndefault: allow\nrules:\n  - id: no-delete\n    tool: delete_file\n    action: deny\n    reason: demo deletion is forbidden\n"), 0600)
	cmd := exec.Command(binary, "setup", "--client", "claude-code", "--config", config, "--server", "fixture", "--policy", policyPath, "--log", logPath, "--output", candidate)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("setup: %v %s", err, out)
	}
	var doc struct {
		Servers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	data, _ := os.ReadFile(candidate)
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	entry := doc.Servers["fixture"]
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_secrets","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete_file","arguments":{"path":"fictional.txt"}}}`,
	}
	recorded := talk(t, entry.Command, entry.Args, requests)
	if !bytes.Contains(recorded[3], []byte("no-delete")) || !bytes.Contains(recorded[3], []byte("-32010")) {
		t.Fatalf("missing deny: %s", recorded[3])
	}
	verified, err := session.VerifyFile(logPath)
	if err != nil || !verified.OK() {
		t.Fatalf("verify: %+v %v", verified, err)
	}
	items, sum, story, err := tui.Load(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Calls != 2 || sum.Denied != 1 || len(story.Denied) != 1 || story.Denied[0].Bypass {
		t.Fatalf("unexpected observations: %+v %+v", sum, story)
	}
	if len(items) == 0 {
		t.Fatal("empty timeline")
	}
	cmd = exec.Command(binary, "inspect", logPath, "--summary")
	if out, err := cmd.CombinedOutput(); err != nil || !bytes.Contains(out, []byte("2 mediated calls")) {
		t.Fatalf("summary: %v %s", err, out)
	}
	if err := os.Rename(server, server+".offline"); err != nil {
		t.Fatal(err)
	}
	replayed := talk(t, binary, []string{"replay", logPath, "--strict"}, requests[:3])
	// Denied requests have no recorded result in the existing replay format.
	// Replay the successful interaction only; denial replay remains a documented miss.
	// Initialization identifies the replay server; compare the recorded tool result.
	for _, i := range []int{2} {
		var a, b any
		_ = json.Unmarshal(recorded[i], &a)
		_ = json.Unmarshal(replayed[i], &b)
		aa, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		if !bytes.Equal(aa, bb) {
			t.Fatalf("replay changed response %d: %s / %s", i, aa, bb)
		}
	}
	// Unknown calls fail loudly even with the upstream unavailable.
	missing := talk(t, binary, []string{"replay", logPath}, []string{`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"not_recorded","arguments":{}}}`})
	if !bytes.Contains(missing[0], []byte("-32011")) {
		t.Fatalf("unexpected miss: %s", missing[0])
	}
}

func TestSetupExistingLogAndMissingExecutable(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "client.json")
	output := filepath.Join(dir, "candidate.json")
	logPath := filepath.Join(dir, "prior recording")
	raw := []byte(`{"mcpServers":{"chosen":{"command":"node","args":[]}}}`)
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--client", "roo", "--config", config, "--server", "chosen", "--output", output, "--log", logPath}
	if _, err := setupRun(append(args, "--tapelog", "nonexistent-tapelog-dx-binary")...); err == nil {
		t.Fatal("missing executable accepted")
	}
	if err := os.WriteFile(logPath, []byte("prior evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := setupRun(args...); err == nil {
		t.Fatal("existing log accepted")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("failed setup created output")
	}
	got, _ := os.ReadFile(logPath)
	if string(got) != "prior evidence" {
		t.Fatal("prior evidence changed")
	}
}

func TestSetupRejectsCandidatePathAliasesBeforeWriting(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "client.json")
	raw := []byte(`{"mcpServers":{"chosen":{"command":"node","args":[]}}}`)
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		output string
		log    string
	}{
		{"same candidate and log", filepath.Join(dir, "same.json"), filepath.Join(dir, "same.json")},
		{"relative absolute alias", filepath.Join(".", filepath.Base(dir), "alias.json"), filepath.Join(dir, "alias.json")},
		{"output is active config", config, filepath.Join(dir, "new-log.jsonl")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, log := tc.output, tc.log
			if tc.name == "relative absolute alias" {
				output = filepath.Join(".", filepath.Base(dir), "alias.json")
				if filepath.VolumeName(dir) != filepath.VolumeName(".") {
					t.Skip("relative alias path is on a different volume")
				}
				cwd, err := os.Getwd()
				if err != nil {
					t.Fatal(err)
				}
				relative, err := filepath.Rel(cwd, filepath.Join(dir, "alias.json"))
				if err != nil {
					t.Skip("cannot form relative alias")
				}
				output = relative
				log = filepath.Join(dir, "alias.json")
			}
			args := []string{"--client", "roo", "--config", config, "--server", "chosen", "--log", log, "--output", output}
			if _, err := setupRun(args...); err == nil {
				t.Fatal("accepted colliding paths")
			}
			got, err := os.ReadFile(config)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatalf("source config changed: %v", err)
			}
			if tc.name != "output is active config" {
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatalf("candidate unexpectedly created at %s", output)
				}
			}
		})
	}
	if runtime.GOOS == "windows" {
		output := filepath.Join(dir, "case.json")
		args := []string{"--client", "roo", "--config", config, "--server", "chosen", "--log", strings.ToUpper(output), "--output", output}
		if _, err := setupRun(args...); err == nil {
			t.Fatal("accepted Windows case-insensitive collision")
		}
	}
	recordings := filepath.Join(dir, "candidate-is-recordings-dir")
	if _, err := setupRun("--client", "roo", "--config", config, "--server", "chosen", "--recordings-dir", recordings, "--output", recordings); err == nil {
		t.Fatal("accepted candidate equal to recordings directory")
	}
	if _, err := os.Lstat(recordings); !os.IsNotExist(err) {
		t.Fatalf("colliding recordings directory unexpectedly created: %v", err)
	}
	got, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatalf("source config changed after recordings-dir collision: %v", err)
	}
}

func TestGeneratedRecordingDirectoryKeepsEveryStart(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	binary := filepath.Join(dir, "tapelog"+suffix)
	server := filepath.Join(dir, "fixture server"+suffix)
	for _, build := range []struct{ out, pkg string }{{binary, "."}, {server, "../../internal/compat/fakecmd"}} {
		cmd := exec.Command("go", "build", "-o", build.out, build.pkg)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, out)
		}
	}
	config := filepath.Join(dir, "client.json")
	candidate := filepath.Join(dir, "candidate.json")
	recordings := filepath.Join(dir, "recordings")
	raw, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fixture": map[string]any{"command": server, "args": []string{"-era", "2026-07-28"}}}})
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}
	setup := exec.Command(binary, "setup", "--client", "claude-code", "--config", config, "--server", "fixture", "--recordings-dir", recordings, "--output", candidate)
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("setup: %v %s", err, out)
	}
	var doc struct {
		Servers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	candidateBytes, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(candidateBytes, &doc); err != nil {
		t.Fatal(err)
	}
	entry := doc.Servers["fixture"]
	requests := []string{`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`}
	logs := make([]string, 0, 2)
	var first []byte
	for i := 0; i < 2; i++ {
		_ = talk(t, entry.Command, entry.Args, requests)
		entries, err := os.ReadDir(recordings)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != i+1 {
			t.Fatalf("start %d left %d logs, want %d", i+1, len(entries), i+1)
		}
		path := ""
		for _, entry := range entries {
			candidatePath := filepath.Join(recordings, entry.Name())
			if i == 0 || candidatePath != logs[0] {
				path = candidatePath
			}
		}
		if path == "" {
			t.Fatal("new start did not create a distinct log")
		}
		logs = append(logs, path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = data
		} else {
			unchanged, err := os.ReadFile(logs[0])
			if err != nil || !bytes.Equal(first, unchanged) {
				t.Fatalf("first session changed on second start: %v", err)
			}
		}
		if result, err := session.VerifyFile(path); err != nil || !result.OK() {
			t.Fatalf("verify start %d: %+v %v", i+1, result, err)
		}
	}
	// A server startup failure still leaves a distinct, valid partial chain.
	failedArgs := append([]string(nil), entry.Args...)
	for i := range failedArgs {
		if failedArgs[i] == server {
			failedArgs[i] = filepath.Join(dir, "missing-server")
			break
		}
	}
	failed := exec.Command(entry.Command, failedArgs...)
	failed.Stdin = strings.NewReader("")
	if out, err := failed.CombinedOutput(); err == nil {
		t.Fatalf("missing upstream unexpectedly started: %s", out)
	}
	entries, err := os.ReadDir(recordings)
	if err != nil || len(entries) != 3 {
		t.Fatalf("failed start did not preserve a separate log: entries=%d err=%v", len(entries), err)
	}
	failedPath := filepath.Join(recordings, entries[2].Name())
	if result, err := session.VerifyFile(failedPath); err != nil || !result.OK() {
		t.Fatalf("failed-start partial log should verify: %+v %v", result, err)
	}
	if got, err := os.ReadFile(logs[0]); err != nil || !bytes.Equal(first, got) {
		t.Fatalf("failed start changed first recording: %v", err)
	}
	if got, err := os.ReadFile(candidate); err != nil || !bytes.Equal(candidateBytes, got) {
		t.Fatalf("recording changed candidate config: %v", err)
	}
}
