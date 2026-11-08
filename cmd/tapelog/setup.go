package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Caseymccallum/tapelog/internal/policy"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var client, config, name, logPath, recordingsDir, output, binary, policyPath string
	var dryRun bool
	cmd := &cobra.Command{
		Use: "setup", Short: "Preview recording for an existing MCP server; optionally save a new config",
		Long: `Choose --client roo, cline, vscode, claude-code or claude-desktop,
then --config <existing JSON file> and --server <existing server name>.
Only a local stdio entry is supported. JSON comments and duplicate keys
are rejected. No client is started and no existing file is changed.

Without --output this is a dry run. --output writes a NEW candidate file;
review it locally, back up your original, then activate it yourself.
Environment variables, credentials, other servers and client settings
are preserved. Preview never prints server arguments or credentials.

By default, each client start creates a unique log under
<config-directory>/tapelog-recordings. Use --recordings-dir to choose
another directory. Explicit --log keeps the legacy fixed-file behavior;
never reuse it for repeated client starts.

  tapelog setup --client roo --config .roo/mcp.json --server filesystem
  tapelog setup --client roo --config .roo/mcp.json --server filesystem --output candidate.json

Only traffic crossing this MCP connection is recorded. Without --policy,
recording has no policy rules (default allow); other boundary protections
remain active. Confirm decisions remain fail closed without an approval path.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rootKey := "mcpServers"
			switch client {
			case "roo", "cline", "claude-code", "claude-desktop":
			case "vscode":
				rootKey = "servers"
			default:
				return fmt.Errorf("choose --client roo, cline, vscode, claude-code or claude-desktop")
			}
			if config == "" || name == "" {
				return fmt.Errorf("provide --config <existing JSON file> and --server <existing name>; see tapelog setup --help")
			}
			f, err := os.Open(config)
			if err != nil {
				return fmt.Errorf("open client config: %w", err)
			}
			raw, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
			f.Close()
			if err != nil {
				return fmt.Errorf("read client config: %w", err)
			}
			if len(raw) > 8*1024*1024 {
				return fmt.Errorf("client config exceeds 8 MiB")
			}
			if err := uniqueJSON(raw); err != nil {
				return fmt.Errorf("client config must be strict JSON with unique keys (JSONC unsupported); original unchanged")
			}
			var doc map[string]json.RawMessage
			if json.Unmarshal(raw, &doc) != nil || doc == nil {
				return fmt.Errorf("client config must be a JSON object")
			}
			var servers map[string]json.RawMessage
			if json.Unmarshal(doc[rootKey], &servers) != nil || servers == nil {
				return fmt.Errorf("expected a %q object for client %s", rootKey, client)
			}
			var entry map[string]json.RawMessage
			if json.Unmarshal(servers[name], &entry) != nil || entry == nil {
				return fmt.Errorf("server %q not found or not an object in %s", name, rootKey)
			}
			var original, transport string
			var argv []string
			if json.Unmarshal(entry["command"], &original) != nil || strings.TrimSpace(original) == "" {
				return fmt.Errorf("selected server needs a nonempty command; only local stdio servers are supported")
			}
			if value, ok := entry["type"]; ok {
				if json.Unmarshal(value, &transport) != nil || transport != "stdio" {
					return fmt.Errorf("only type stdio is supported; remote transports need tapelog mux")
				}
			}
			if _, ok := entry["url"]; ok {
				return fmt.Errorf("URL servers need tapelog mux; setup supports local stdio only")
			}
			if value, ok := entry["args"]; ok {
				var typed []any
				if json.Unmarshal(value, &typed) != nil || typed == nil {
					return fmt.Errorf("selected server args must be an array of strings")
				}
				for _, arg := range typed {
					if _, ok := arg.(string); !ok {
						return fmt.Errorf("selected server args must be an array of strings")
					}
				}
				if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &argv) != nil {
					return fmt.Errorf("selected server args must be an array of strings")
				}
			}
			base := strings.TrimSuffix(strings.ToLower(filepath.Base(original)), ".exe")
			if base == "tapelog" {
				return fmt.Errorf("selected server already uses tapelog; refusing to wrap it twice")
			}
			if binary == "" {
				binary, err = os.Executable()
			} else {
				binary, err = exec.LookPath(binary)
			}
			if err != nil {
				return fmt.Errorf("find Tapelog executable: %w", err)
			}
			binary, err = filepath.Abs(binary)
			if err != nil {
				return err
			}
			if strings.ContainsRune(binary, '\x00') || strings.ContainsRune(original, '\x00') {
				return fmt.Errorf("command contains an invalid NUL character")
			}
			uniqueLogs := !cmd.Flags().Changed("log")
			if uniqueLogs {
				if recordingsDir == "" {
					recordingsDir = filepath.Join(filepath.Dir(config), "tapelog-recordings")
				}
				recordingsDir, err = filepath.Abs(recordingsDir)
				if err != nil {
					return err
				}
				if st, statErr := os.Stat(recordingsDir); statErr == nil && !st.IsDir() {
					return fmt.Errorf("recordings path exists and is not a directory")
				} else if statErr != nil && !os.IsNotExist(statErr) {
					return fmt.Errorf("check recordings directory: %w", statErr)
				}
				if st, statErr := os.Stat(filepath.Dir(recordingsDir)); statErr != nil || !st.IsDir() {
					return fmt.Errorf("recordings directory parent must already exist")
				}
			} else {
				if recordingsDir != "" {
					return fmt.Errorf("--log and --recordings-dir cannot be used together")
				}
				if logPath == "" {
					logPath = "tapelog-first.jsonl"
				}
				logPath, err = filepath.Abs(logPath)
				if err != nil {
					return err
				}
				if _, err = os.Lstat(logPath); err == nil {
					return fmt.Errorf("log already exists; choose a fresh --log path to preserve earlier recordings")
				} else if !os.IsNotExist(err) {
					return fmt.Errorf("check log path: %w", err)
				}
				if st, statErr := os.Stat(filepath.Dir(logPath)); statErr != nil || !st.IsDir() {
					return fmt.Errorf("log parent must already be a directory; create it first")
				}
			}
			if output != "" {
				if err := validateSetupPaths(config, output, recordingsDir); err != nil {
					return err
				}
				if !uniqueLogs {
					if err := validateDistinctPaths(output, logPath); err != nil {
						return err
					}
				}
			}
			wrapped := []string{"record"}
			if uniqueLogs {
				wrapped = append(wrapped, "--recordings-dir", recordingsDir)
			} else {
				wrapped = append(wrapped, "--log", logPath)
			}
			wrapped = append(wrapped, "--harness", client)
			if policyPath != "" {
				policyPath, err = filepath.Abs(policyPath)
				if err != nil {
					return err
				}
				if _, err := policy.Load(policyPath); err != nil {
					return fmt.Errorf("policy did not validate; original unchanged: %w", err)
				}
				wrapped = append(wrapped, "--policy", policyPath)
			}
			wrapped = append(wrapped, "--", original)
			wrapped = append(wrapped, argv...)
			entry["command"], _ = json.Marshal(binary)
			entry["args"], _ = json.Marshal(wrapped)
			servers[name], _ = json.Marshal(entry)
			doc[rootKey], _ = json.Marshal(servers)
			candidate, err := json.MarshalIndent(doc, "", "  ")
			if err != nil {
				return err
			}
			location := logPath
			if uniqueLogs {
				location = recordingsDir + "\\<new unique recording per start>"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Preview: wrap server %q for %s with Tapelog record.\nConfig: %s\nRecording: %s\nOnly command/args change; other servers, env and settings are preserved.\nArguments and credentials are omitted from this preview.\n", name, client, config, location)
			if policyPath == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "No policy selected: default allow, with existing boundary protections. Use --policy to retain your chosen enforcement rules.")
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Confirm verdicts are not auto-approved. Configure the existing loopback approval queue if your policy requires human review.")
			if runtime.GOOS == "windows" {
				fmt.Fprintln(cmd.OutOrStdout(), "Windows: argv is preserved without shell rewriting. Batch/npx launchers and Roo/Cline quoting require client testing; see docs/FIRST-SESSION.md. No 8.3 names are assumed.")
			}
			if output == "" || dryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "Dry run: no files written. Use --output <new file> to save a candidate.")
				return nil
			}
			if err := writeNewConfig(output, append(candidate, '\n')); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved candidate: %s\nContains the original credentials: keep it private. Review locally; back up %s before manually activating. Undo by restoring that backup. No client settings were activated.\n", output, config)
			if uniqueLogs {
				fmt.Fprintf(cmd.OutOrStdout(), "Each client start creates a separate recording in %s. Run `tapelog inspect <recording>.jsonl --summary`, `tapelog verify <recording>.jsonl`, or `tapelog web --dir %s`.\n", shellArg(recordingsDir), shellArg(recordingsDir))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Restart the selected server in your client, explicitly call one of its MCP tools, then:\n  tapelog inspect %s --summary\n  tapelog verify %s\n  tapelog web --dir %s\nReplay as an MCP server (waits for client input): tapelog replay %s --strict\n", shellArg(logPath), shellArg(logPath), shellArg(filepath.Dir(logPath)), shellArg(logPath))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&client, "client", "", "client: roo, cline, vscode, claude-code, claude-desktop")
	cmd.Flags().StringVar(&config, "config", "", "existing client configuration (strict JSON)")
	cmd.Flags().StringVar(&name, "server", "", "existing stdio server name")
	cmd.Flags().StringVar(&logPath, "log", "", "legacy fixed session log path (one recording only; repeated starts overwrite)")
	cmd.Flags().StringVar(&recordingsDir, "recordings-dir", "", "directory for one unique, non-overwriting recording per start")
	cmd.Flags().StringVar(&binary, "tapelog", "", "Tapelog executable (default current executable)")
	cmd.Flags().StringVar(&policyPath, "policy", "", "existing policy to validate and use; never edited")
	cmd.Flags().StringVar(&output, "output", "", "NEW candidate JSON file; never replaces existing files")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview only, even with --output")
	return cmd
}

func validateSetupPaths(config, output, recordingsDir string) error {
	configPath, err := canonicalPath(config)
	if err != nil {
		return fmt.Errorf("resolve client config path: %w", err)
	}
	outputPath, err := canonicalPath(output)
	if err != nil {
		return fmt.Errorf("resolve candidate path: %w", err)
	}
	if samePath(configPath, outputPath) {
		return fmt.Errorf("candidate output resolves to the active client configuration; choose a different --output")
	}
	if recordingsDir != "" {
		logDir, err := canonicalPath(recordingsDir)
		if err != nil {
			return fmt.Errorf("resolve recordings directory: %w", err)
		}
		if samePath(logDir, outputPath) || samePath(logDir, configPath) {
			return fmt.Errorf("candidate, active client config and recordings directory must be separate paths")
		}
	}
	return nil
}

func validateDistinctPaths(a, b string) error {
	left, err := canonicalPath(a)
	if err != nil {
		return err
	}
	right, err := canonicalPath(b)
	if err != nil {
		return err
	}
	if samePath(left, right) {
		return fmt.Errorf("candidate output and recording path resolve to the same location")
	}
	return nil
}

// canonicalPath resolves existing symlink ancestors as well as lexical aliases.
func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(resolved), nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs, nil
	}
	resolvedParent, err := canonicalPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(abs)), nil
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Reject duplicate keys at every depth so parsing cannot silently discard settings.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var value func() error
	value = func() error {
		token, err := d.Token()
		if err != nil {
			return err
		}
		switch token {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return err
				}
				k, ok := key.(string)
				if !ok || seen[k] {
					return fmt.Errorf("duplicate key")
				}
				seen[k] = true
				if err := value(); err != nil {
					return err
				}
			}
			token, err = d.Token()
			if err != nil || token != json.Delim('}') {
				return fmt.Errorf("invalid object")
			}
		case json.Delim('['):
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
			token, err = d.Token()
			if err != nil || token != json.Delim(']') {
				return fmt.Errorf("invalid array")
			}
		}
		return nil
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing data")
	}
	return nil
}

func writeNewConfig(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("create NEW candidate (existing files are never replaced): %w", err)
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

// Display one argument for the native documented shell, not JSON/Go escaping.
func shellArg(value string) string {
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
