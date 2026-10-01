/**
 * tapelog adapter for TypeScript MCP clients (thin, dependency-free).
 *
 * Wraps an MCP stdio server config so all traffic flows through
 * `tapelog record` — session logging + policy enforcement without
 * touching your agent code:
 *
 *   import { withTapelog } from "./tapelog";
 *
 *   const servers = {
 *     filesystem: withTapelog(
 *       { command: "npx", args: ["-y", "@modelcontextprotocol/server-filesystem", "."] },
 *       { policy: "policy.yaml", denyOnDrift: true },
 *     ),
 *   };
 */

export interface McpServerConfig {
  command: string;
  args?: string[];
  env?: Record<string, string>;
}

export interface TapelogOptions {
  /** Policy YAML file. Omit for no-policy recording (default allow; boundary protections stay active). */
  policy?: string;
  /** Session log output path (default: session.jsonl). */
  log?: string;
  /** Task label — enables task-scoped policy rules. */
  task?: string;
  /** Treat `confirm` verdicts as allow (recorded). */
  autoConfirm?: boolean;
  /** Deny tool calls whose descriptor changed since first seen. */
  denyOnDrift?: boolean;
  /** Harness name recorded in the session log. */
  harness?: string;
  /** Path to the tapelog binary (default: "tapelog" on PATH). */
  binary?: string;
}

/** Wrap an MCP server config so tapelog mediates and records it. */
export function withTapelog(
  server: McpServerConfig,
  opts: TapelogOptions = {},
): McpServerConfig {
  const args = ["record"];

  if (opts.policy) args.push("--policy", opts.policy);
  if (opts.log) args.push("--log", opts.log);
  if (opts.task) args.push("--task", opts.task);
  if (opts.harness) args.push("--harness", opts.harness);
  if (opts.autoConfirm) args.push("--auto-confirm");
  if (opts.denyOnDrift) args.push("--deny-on-drift");

  args.push("--", server.command, ...(server.args ?? []));

  return {
    command: opts.binary ?? "tapelog",
    args,
    env: server.env,
  };
}
