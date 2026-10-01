# Signed session checkpoints

`tapelog checkpoint` pins a session's chain head with a **signature**
(proves WHO attested it) and a **transparency-log witness** (proves
WHEN). Together they make "this trajectory existed in this exact form at
this time" true — the claim a bare hash chain cannot make (see
`spec/FAQ.md`: anyone who rewrites the whole log can recompute every
hash; `verify --expect` closes that with an out-of-band anchor, and
checkpoints close it *self-describing*).

## Quick start

```powershell
# sign + witness (SSH key)
tapelog checkpoint session.jsonl --signer ssh --key ~/.ssh/id_ed25519 --out session.ckpt.json

# later, anywhere: verify the log against the attestation (offline)
tapelog verify session.jsonl --checkpoint session.ckpt.json
```

The verify output:

```
OK: session.jsonl
  19 events, chain intact
  chain head: a6405c2b…
  checkpoint: seq 19, signed by ssh (SHA256:2e6u…), witnessed by rekor
```

## What the checkpoint contains

```json
{
  "version": 1,
  "session_id": "20261001T120113.598-210132e7f83037c8",
  "seq": 19,
  "chain_head": "a6405c2b…",
  "created_at": "2026-10-01T12:00:00.000Z",
  "signer":   { "type": "ssh", "identity": "SHA256:…", "public_key": "ssh-ed25519 AAAA…" },
  "signature": "-----BEGIN SSH SIGNATURE-----…",
  "witness":  { "type": "rekor", "entry_uuid": "…", "log_index": …, "integrated_time": …,
                "body": "…", "signed_entry_timestamp": "…", "inclusion_proof": { … } }
}
```

The signed bytes are the canonical statement (spec-style, one field per
line) so any implementation can re-verify:

```
tapelog-checkpoint-v1
session_id=<id>
seq=<n>
chain_head=<hex>
created_at=<RFC3339>
```

## What verification checks

1. **Chain** — the log hashes cleanly and event `seq`'s hash equals
   `chain_head` (the log may continue past `seq`; periodic checkpoints
   for long sessions).
2. **Signature (WHO)** — SSH: SSHSIG over the statement, interop with
   `ssh-keygen -Y verify` (namespace `file`, sha512). Cosign: the
   sigstore bundle via `cosign verify-blob` (keyless identity = the
   Fulcio cert SAN + OIDC issuer).
3. **Witness (WHEN)** — for `--witness rekor`, fully offline:
   - *entry binding*: the log entry hashes the exact statement and
     carries the exact signature + key (no substitution),
   - *SET*: the log's signature over the entry (the "when" claim),
   - *RFC 6962 inclusion* against the *signed tree head*.

## Signers

| `--signer` | key | notes |
|---|---|---|
| `ssh` | `--key <openssh private key>` | SSHSIG; verify with tapelog or `ssh-keygen -Y verify -f allowed_signers -I <id> -n file -s sig` |
| `cosign` | keyless (OIDC) or `cosign` keys | requires the `cosign` CLI; the bundle carries its own Rekor witness |

## Witness options

- `--witness rekor` (default): submit to `https://rekor.sigstore.dev`
  (`--rekor-url` for others). Verification is offline afterwards — the
  checkpoint stores everything needed.
- `--witness none`: explicit opt-out. `verify` prints a loud warning:
  **signed but NOT witnessed — the "when" is unproven** (someone with
  the signing key could have backdated it).

The trust root for witness verification is the log's public key (pinned
default for rekor.sigstore.dev in `internal/checkpoint`; override for
other logs or key rotation with the checkpoint package's
`VerifyOptions.LogPublicKeyPEM`).

## Operational guidance

- **Periodic checkpoints** for long sessions: run `checkpoint --seq N`
  at intervals; each pins a verified prefix.
- **CI**: checkpoint at the end of a job and upload `*.checkpoint.json`
  as an artifact — the artifact is the self-contained attestation.
- `verify --expect` remains the zero-dependency anchor; a checkpoint is
  the stronger artifact for evidence you hand to someone else.

## Interop notes

- SSH signatures use the OpenSSH SSHSIG format (PROTOCOL.sshsig,
  namespace `file`, **sha512** — OpenSSH's default; the public Rekor
  rejects other hash algorithms in `rekord` entries).
- `ssh-keygen -Y verify -f allowed_signers -I <identity> -n file -s
  <sigfile>` reads the statement from **stdin**.
- Rekor entry type: `rekord` v0.0.1 with `signature.format: "ssh"` and
  the statement as `data.content` (the log stores only its hash).