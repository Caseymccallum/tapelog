// A source-checkout demonstration, using Tapelog's existing hermetic MCP fixture.
// No external server, account or network service is used.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Caseymccallum/tapelog/internal/compat"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "demo:", err)
		os.Exit(1)
	}
}

func run() error {
	binary := flag.String("tapelog", "tapelog", "installed Tapelog executable or absolute build path")
	out := flag.String("out", "", "NEW output directory; default a fresh temporary directory")
	fixture := flag.Bool("fixture", false, "internal demo server mode")
	flag.Parse()
	if *fixture {
		server := compat.NewServer(compat.Era2026)
		scan := bufio.NewScanner(os.Stdin)
		for scan.Scan() {
			if response := server.Handle(scan.Bytes()); len(response) > 0 {
				if _, err := os.Stdout.Write(response); err != nil {
					return err
				}
			}
		}
		return scan.Err()
	}
	started := time.Now()
	exe, err := exec.LookPath(*binary)
	if err != nil {
		return fmt.Errorf("find Tapelog: %w; build it or pass --tapelog <absolute path>", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}
	var dir string
	if *out == "" {
		dir, err = os.MkdirTemp("", "tapelog-first-session-*")
	} else {
		dir, err = filepath.Abs(*out)
		if err == nil {
			err = os.Mkdir(dir, 0700)
		}
	}
	if err != nil {
		return fmt.Errorf("create NEW demo directory (existing directories are refused): %w", err)
	}
	transcript, err := os.OpenFile(filepath.Join(dir, "transcript.txt"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer transcript.Close()
	writer := io.MultiWriter(os.Stdout, transcript)
	fmt.Fprintln(writer, "Tapelog first session: scripted MCP activity, not an LLM or live client acceptance test.")
	self, err := os.Executable()
	if err != nil {
		return err
	}
	config := filepath.Join(dir, "client.json")
	candidate := filepath.Join(dir, "candidate.json")
	recordingsDir := filepath.Join(dir, "tapelog-recordings")
	var logPath string
	policyPath := filepath.Join(dir, "demo-policy.yaml")
	raw, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"demo": map[string]any{"command": self, "args": []string{"--fixture"}}}}, "", "  ")
	if err := os.WriteFile(config, raw, 0600); err != nil {
		return err
	}
	// Only fictional fixture tools are involved. Never edit an owner's policy.
	if err := os.WriteFile(policyPath, []byte("version: 1\ndefault: allow\nrules:\n  - id: demo-no-delete\n    tool: delete_file\n    action: deny\n    reason: deletion is not permitted in this demo\n"), 0600); err != nil {
		return err
	}
	command := func(args ...string) error {
		fmt.Fprintf(writer, "\n> tapelog %s\n", args[0])
		cmd := exec.Command(exe, args...)
		cmd.Stdout = writer
		cmd.Stderr = writer
		return cmd.Run()
	}
	if err := command("doctor", "--server", self, "--log", logPath); err != nil {
		return err
	}
	if err := command("setup", "--client", "claude-code", "--config", config, "--server", "demo", "--policy", policyPath, "--recordings-dir", recordingsDir, "--output", candidate); err != nil {
		return err
	}
	var doc struct {
		Servers map[string]struct {
			Command string
			Args    []string
		} `json:"mcpServers"`
	}
	generated, err := os.ReadFile(candidate)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(generated, &doc); err != nil {
		return err
	}
	entry := doc.Servers["demo"]
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"fictional.txt"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete_file","arguments":{"path":"fictional.txt"}}}`,
	}
	fmt.Fprintln(writer, "\n> record through generated configuration (one allowed call, one denial)")
	recorded, err := conversation(writer, entry.Command, entry.Args, requests)
	if err != nil {
		return err
	}
	logs, err := os.ReadDir(recordingsDir)
	if err != nil {
		return err
	}
	for _, log := range logs {
		if !log.IsDir() && filepath.Ext(log.Name()) == ".jsonl" {
			logPath = filepath.Join(recordingsDir, log.Name())
			break
		}
	}
	if logPath == "" {
		return fmt.Errorf("recording did not create a session log in %s", recordingsDir)
	}
	if !bytes.Contains(recorded[3], []byte("demo-no-delete")) {
		return fmt.Errorf("expected policy denial missing")
	}
	for _, args := range [][]string{{"inspect", logPath, "--plain"}, {"verify", logPath}, {"policy", "whatif", "--policy", policyPath, logPath}} {
		if err := command(args...); err != nil {
			return err
		}
	}
	scenario := filepath.Join(dir, "scenario.yaml")
	quoted, _ := json.Marshal(logPath)
	if err := os.WriteFile(scenario, []byte(fmt.Sprintf("version: 1\nscenario: first-session\nfixture: %s\nassert:\n  - called: {tool: read_file, times: 1}\n  - denied: delete_file\n  - invariant: no_deny_bypassed\n", quoted)), 0600); err != nil {
		return err
	}
	if err := command("test", "--plain", scenario); err != nil {
		return err
	}
	fmt.Fprintln(writer, "\n> replay recorded response, without starting the original MCP server")
	replayed, err := conversation(writer, exe, []string{"replay", logPath, "--strict"}, requests[:3])
	if err != nil {
		return err
	}
	var a, b any
	_ = json.Unmarshal(recorded[2], &a)
	_ = json.Unmarshal(replayed[2], &b)
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if !bytes.Equal(aa, bb) {
		return fmt.Errorf("replay response differed")
	}
	idle := filepath.Join(dir, "discovery-only.jsonl")
	fmt.Fprintln(writer, "\n> record discovery only, then explain zero mediated calls")
	if _, err := conversation(writer, exe, []string{"record", "--log", idle, "--", self, "--fixture"}, requests[:2]); err != nil {
		return err
	}
	if err := command("inspect", idle, "--summary"); err != nil {
		return err
	}
	fmt.Fprintf(writer, "\nPASS: recorded result replayed identically. Denied calls have no recorded result and are not replayed; repeating one would be a miss.\nRecording: %s\nArtifacts: %s\nMeasured demo runtime: %s (excludes installation and go run compilation; not a five-minute onboarding measurement).\nOpen the local dashboard with tapelog web --dir %s.\nThe generated candidate references a temporary demo executable; it is NOT a persistent client configuration.\n", logPath, dir, time.Since(started).Round(time.Millisecond), recordingsDir)
	return nil
}

func conversation(writer io.Writer, binary string, args, requests []string) ([]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stderr = writer
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
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
		if _, err := io.WriteString(in, request+"\n"); err != nil {
			return nil, err
		}
		if !scan.Scan() {
			return nil, fmt.Errorf("MCP response missing: %v", scan.Err())
		}
		response := append(json.RawMessage(nil), scan.Bytes()...)
		responses = append(responses, response)
		fmt.Fprintln(writer, string(response))
	}
	_ = in.Close()
	return responses, cmd.Wait()
}
