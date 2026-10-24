# Security

ghv is a security tool, so a bug that lets an agent write to GitHub without your approval, read the write token, or fool `ghv-apply` into sending something other than what you saw counts as a vulnerability. So does anything that breaks one of the invariants in [AGENTS.md](AGENTS.md).

## Reporting

Report it privately through GitHub: https://github.com/Sriram-PR/ghv/security/advisories/new. Please don't open a public issue for it.

Include what you ran, what happened, and what you expected, and say whether the agent was running as its own user ([docs/containment.md](docs/containment.md)) or as you. ghv is a one-person project; I answer as soon as I can. I'll tell you when a fix is out and credit you in the release notes unless you'd rather not be named.

## What's out of scope

These are known limits, written down in the README, not bugs:

- An agent running as your own user can read `ghv-apply`'s memory, type into your terminal, or read your clipboard. ghv stops mistakes in that setup, not attacks; `ghv doctor` reports it as `same-user`.
- A hijacked agent can read anything the read token can and put it in a draft. ghv flags secrets it spots in drafts but doesn't stop that.
- ghv never pushes. Your SSH key and `git push` are outside it.
- Approving a harmful draft after reading it.

## Supported versions

Only the latest v0.x release gets fixes.
