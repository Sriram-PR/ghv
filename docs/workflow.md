# Contributing to a repository with ghv

The agent does the reading and writes the drafts. You send the drafts with `ghv-apply`, and you do the one write ghv never does: `git push`. This is the loop for a pull request to someone else's repository, with the agent running as `ghv-agent` ([containment.md](containment.md)). `you` is your GitHub login.

## 1. The agent reads

Everything here works without asking you:

```sh
ghv issue view 4321 -R owner/project
ghv issue activity 4321 -R owner/project --no-bots
ghv search issues 'repo:owner/project is:open label:"good first issue"' --limit 20
ghv file owner/project CONTRIBUTING.md
ghv pr list -R owner/project --state all --limit 10
```

The agent clones over anonymous https into a directory it owns, and works on a branch there:

```sh
git clone https://github.com/owner/project.git
git switch -c fix/parser-crash
```

## 2. The agent drafts a fork

If you have no fork yet:

```sh
ghv fork owner/project
```

That prints `Drafted d-...: ask the human to run ghv-apply.` Nothing has been sent.

## 3. You send it

In your own terminal:

```sh
/usr/local/bin/ghv-apply
```

It asks for your passphrase once, then shows each waiting draft with its context and the exact request it would send. Answer `a` to approve, `e` to edit the text first, `r` to reject with a reason the agent can read, `s` to skip it for now, or `q` to stop. After a fork it waits up to a minute for GitHub to finish making it.

## 4. You push the agent's branch

Don't run git inside the agent's checkout: a repository's config and hooks can run commands, and the agent controls them. Have the agent pack the branch into a bundle instead, which is plain data:

```sh
git bundle create /tmp/parser-crash.bundle fix/parser-crash
```

Then in your own clone of your fork:

```sh
git bundle list-heads /tmp/parser-crash.bundle
git fetch /tmp/parser-crash.bundle fix/parser-crash:fix/parser-crash
git log -p origin/main..fix/parser-crash
git push origin fix/parser-crash
```

Read the commits before you push. This is the only place the agent's code reaches GitHub, and it goes under your SSH key.

## 5. The agent drafts the pull request

```sh
ghv pr create -R owner/project --head you:fix/parser-crash --title "Fix a crash on empty input" --body-file pr-body.md
```

The draft pins the branch's current commit. If you push again before sending it, `ghv-apply` blocks the stale draft (`you:fix/parser-crash has moved from A to B since the draft was made; ask for a new draft`) and the agent drafts it again.

Run `ghv-apply` again. A pull request's preview shows its code; press `d` for the full diff.

## 6. The agent follows up

The agent checks what happened to its drafts rather than assuming:

```sh
ghv drafts
```

Each draft shows as waiting, sent with its URL, or `rejected: <your reason>`. Then it watches the pull request:

```sh
ghv pr checks 9876 -R owner/project --watch
ghv pr activity 9876 -R owner/project --since 2d --no-bots
```

Replies go back as drafts too:

```sh
ghv comment 9876 -R owner/project --body-file reply.md
ghv review reply 9876 2468013579 -R owner/project --body-file answer.md
ghv pr edit 9876 -R owner/project --title "Fix a crash on empty or blank input"
```

For a review change, the agent commits on the same branch and bundles it again; you fetch, read, and push as in step 4.

## Your record

`ghv-apply log` lists everything you've sent. `ghv-apply log --verify` compares that with your public events on GitHub and flags anything made outside `ghv-apply`, which is how you'd notice a token you didn't know was loose.
