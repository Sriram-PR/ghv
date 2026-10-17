# AGENTS.md: ghv

`ghv` gives AI coding agents full read access to GitHub and no way to write to it. Every write is a draft a human reviews and sends with `ghv-apply`.

## Security invariants

These hold in every commit. A change that breaks one is wrong, however useful.

1. **`ghv` cannot write.** `cmd/ghv` and everything it imports send only `GET` requests and GraphQL query documents. Write code lives in `internal/write`, reachable only from `cmd/ghv-apply`. The import-graph test enforces this; keep it passing and keep it strict. This protects against slip-ups; the hard barrier is that the read token itself cannot write: a classic token with no scopes (verified from an empty `X-OAuth-Scopes`), or a read-only fine-grained token the human has confirmed.
2. **`ghv` is Go standard library only.** Third-party modules (`golang.org/x/crypto`, `golang.org/x/term`, `golang.org/x/sys`) belong to `ghv-apply` alone.
3. **`ghv-apply` sends exactly what the human saw.** Read each draft once, render from memory, send from the same value. Build requests from the six typed action kinds; a draft never names a method or path.
4. **Drafts are untrusted input.** Validate strictly, reject unknown fields, fetch review context live from GitHub, never from the draft.
5. **`ghv-apply` trusts nothing the agent can write.** Review context comes from live, unauthenticated GETs: no `ghv` cache, no conditional requests, never the read token. On start it re-execs with an allowlisted environment, dropping `HOME`, `XDG_*`, proxy, and TLS variables, so no agent-controlled path or setting reaches it.
6. **Secrets stay in memory, briefly.** The write token is decrypted for one review session, held as `[]byte`, zeroed when the session ends. The passphrase and every approval decision come only from `/dev/tty`; the passphrase is never cached across sessions or logged.
7. **No network call the user did not ask for.** No telemetry, no update checks, no background requests.

## Workflow

Write the failing test first; for the invariants above, the test is the deliverable.

## Conventions

- Conventional Commits (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `chore:`, `ci:`, `build:`).
- Errors to stderr; `--json` output stays a stable, documented schema.
- Reproducible builds: `CGO_ENABLED=0`, `-trimpath`.
- Comment only where the code does not explain itself.
