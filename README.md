# ghv

ghv gives an AI coding agent full read access to GitHub and no way to write to it. Every write the agent wants (a fork, a pull request, a comment, an issue, an edit, a review reply) becomes a draft. You read the draft in your own terminal and send it with `ghv-apply`, using a write token only your passphrase unlocks.

It isn't a sandbox. With the agent running as its own user, every GitHub write is a reviewed, human-approved act. With the agent running as you, ghv stops mistakes, not attacks. `ghv doctor` tells you which of the two you have.

| Mode | Agent runs as | A slip-up | A hijacked agent |
|---|---|---|---|
| **Contained** (recommended) | A separate Unix user, with no access to your terminals, clipboard, session bus, SSH keys, browser profiles, or ghv's vault and audit log | Covered | Covered for writes. It can still read public data and put it in a draft |
| **Uncontained** | You | Covered | **Not covered.** A process running as you can read `ghv-apply`'s memory, log your keystrokes, read your clipboard, or draw over your terminal |

A hijacked agent is one following instructions it picked up from an issue body, a PR comment, or a CI log. Setting up Contained mode takes a few minutes with sudo: [docs/containment.md](docs/containment.md).

## What it guarantees, with the agent contained

- **The agent holds no GitHub credential that can write.** Its read token is a classic token with no scopes, which ghv checks on every response. The `ghv` binary has no write code in it at all; a test on the import graph keeps it that way.
- **`ghv-apply` sends only what you reviewed.** A draft is one of six kinds of request, built from checked fields, and shown in full first, including a pull request's code.
- **`ghv-apply` trusts nothing the agent can write.** It reads the draft's context live from GitHub, ignores the agent's cache and token, pins your GitHub identity in the vault, and resets its own environment before it starts.
- **The audit log is yours.** It lives in your home, out of the agent's reach, and `ghv-apply log --verify` checks it against your public events on GitHub.

What it doesn't do: stop the agent from reading what the read token can read, push code (you push, see [docs/workflow.md](docs/workflow.md)), or stop you approving something bad.

## Install

Download `ghv_vX.Y.Z_OS_ARCH`, `ghv-apply_vX.Y.Z_OS_ARCH`, and `SHA256SUMS` from the [latest release](https://github.com/Sriram-PR/ghv/releases), check them, and install both root-owned:

```sh
sha256sum --ignore-missing -c SHA256SUMS
sudo install -o root -m 755 ghv_v0.1.0_linux_amd64 /usr/local/bin/ghv
sudo install -o root -m 755 ghv-apply_v0.1.0_linux_amd64 /usr/local/bin/ghv-apply
```

On macOS, check the two files you downloaded with `grep _darwin_arm64 SHA256SUMS | shasum -a 256 -c` (or `_darwin_amd64` on an Intel Mac). v0.x releases aren't signed: the checksums catch a broken download, not a swapped one. Signed releases come before v1.

Or build from source with Go 1.27, then install the two binaries the same way:

```sh
CGO_ENABLED=0 go build -trimpath -o . ./cmd/ghv ./cmd/ghv-apply
```

`ghv-apply` has to be a file the agent can't replace, so keep it root-owned and run it by its full path, `/usr/local/bin/ghv-apply`. Linux and macOS are supported; Windows isn't.

## Setup

1. **A read token** for the agent: a classic token with no scopes ([make one](https://github.com/settings/tokens/new), tick nothing), saved to `~/.config/ghv/read-token` with mode 600 in the agent's home. Check it with `ghv token`. Without one, ghv reads anonymously at 60 requests an hour.
2. **A write token** for you: a classic token with only `public_repo`, 90 days. Store it with `/usr/local/bin/ghv-apply init`, which asks for the token and then gives you a six-word passphrase. The token never touches disk unencrypted.
3. **Contain the agent**: [docs/containment.md](docs/containment.md). Then check from the agent's user:

```sh
sudo -u ghv-agent /usr/local/bin/ghv-apply doctor --human-user you --login your-github-login
```

## Use

The agent reads:

```sh
ghv pr view 1638 -R reticlehq/reticle
ghv pr diff 1638 -R reticlehq/reticle
ghv pr checks 1638 -R reticlehq/reticle --watch
ghv issue list -R cli/cli --label bug --since 7d
ghv search code 'repo:cli/cli NewCmdRoot'
ghv run logs 123456789 -R cli/cli --failed
ghv status
```

The agent drafts, which sends nothing:

```sh
ghv comment 1638 -R reticlehq/reticle --body-file reply.md
ghv pr create -R reticlehq/reticle --head you:fix-typo --title "Fix a typo in the README" --body-file pr.md
ghv drafts
```

You review and send:

```sh
/usr/local/bin/ghv-apply
```

For each draft it shows the text, anything risky in it (hidden characters, bot mentions and commands, closing keywords, things that look like secrets, and every link target), the live context from GitHub, and the exact request. You approve, edit, reject with a reason the agent sees in `ghv drafts`, or skip.

`ghv --help` lists every command, and `ghv pr create --help` (for example) shows one command's flags. Every command takes `--json`. The full contributor loop, fork to pull request, is in [docs/workflow.md](docs/workflow.md).

## For agents

`skills/ghv/SKILL.md` teaches an agent the commands and the rules. Copy the `skills/ghv` directory into your agent's skills directory (for Claude Code, `~/.claude/skills/`). Then add this to your `AGENTS.md` or `CLAUDE.md`:

```
GitHub: use ghv for every read, and draft every write with it (the ghv skill has the commands); the human sends drafts with ghv-apply. Don't use gh, and don't call the GitHub API with curl or any other token.
```

## Why

I wanted an agent to help with open source contributions without handing it a token that can post as me, and without `gh`'s telemetry. Tools that limit an agent with configuration, like a read-only mode, rely on a file the agent's own user can edit. ghv splits the credentials instead: the agent only ever has a token that can't write, and the one that can is locked behind a passphrase only you type.

It covers the small part of `gh` a contributor's daily loop uses. It makes no network call you didn't ask for: no telemetry, no update checks.

## More

- [docs/containment.md](docs/containment.md): running the agent as its own user
- [docs/workflow.md](docs/workflow.md): from an issue to a merged pull request
- [docs/smoke-test.md](docs/smoke-test.md): the manual test before a release
- [SECURITY.md](SECURITY.md): reporting a vulnerability

## License

Apache-2.0, see [LICENSE](LICENSE). The passphrase generator uses the [EFF large wordlist](https://www.eff.org/dice), by the Electronic Frontier Foundation, under CC BY 3.0.
