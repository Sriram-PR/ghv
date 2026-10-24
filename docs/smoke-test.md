# Smoke test against a throwaway repository

CI never talks to GitHub. Before a release, run this by hand against a repository you make for the purpose and delete afterwards. It sends real writes, so use a repository nobody else watches. It takes about 15 minutes.

You need the binaries for the release installed root-owned in `/usr/local/bin`, a read token at `~/.config/ghv/read-token` (a classic token with no scopes), and a throwaway write token: a classic token with only `public_repo`, expiring in a day or two, from https://github.com/settings/tokens/new. Below, `you` is your GitHub login and `you/ghv-smoke` the throwaway repository, made public on github.com with a README so it has a `main` branch.

## Reads

```sh
ghv version
ghv token
ghv rate
ghv repo view you/ghv-smoke
ghv file you/ghv-smoke README.md
ghv search repos 'user:you ghv-smoke'
ghv status
```

`ghv token` should report `read-only`. Each command should print something sensible and exit 0.

## The vault

```sh
/usr/local/bin/ghv-apply init
```

Paste the write token, accept the generated passphrase, and type it again. It should say who the token belongs to and when it expires, then `Saved the vault to ...`.

## One draft of each kind

Make a branch on the repository for the pull request, from a clone of it, pushed with your own git:

```sh
git switch -c smoke
echo smoke >> README.md
git commit -am "smoke test"
git push origin smoke
```

Then draft:

```sh
printf 'Smoke test issue.\n' > /tmp/issue.md
ghv issue create -R you/ghv-smoke --title "Smoke test" --body-file /tmp/issue.md
printf 'Smoke test pull request.\n' > /tmp/pr.md
ghv pr create -R you/ghv-smoke --head you:smoke --title "Smoke test" --body-file /tmp/pr.md
ghv drafts
```

Both should be `waiting for ghv-apply`.

## Send them

```sh
/usr/local/bin/ghv-apply
```

Enter the passphrase. For the issue, check the preview, then approve it. For the pull request, press `d` to see its diff, then approve it. Each should end with `Sent: URL`. Open both URLs.

## Follow-ups on what you sent

Use the numbers from the URLs; here the issue is 1 and the pull request 2.

```sh
printf 'Smoke test comment.\n' > /tmp/comment.md
ghv comment 1 -R you/ghv-smoke --body-file /tmp/comment.md
ghv pr edit 2 -R you/ghv-smoke --title "Smoke test, edited"
printf 'To be rejected.\n' > /tmp/reject.md
ghv comment 2 -R you/ghv-smoke --body-file /tmp/reject.md
```

Run `ghv-apply` again:

- the comment on 1: press `e`, change the text in your editor, check that the new text shows with `Edited.`, then approve it;
- the title edit: approve it;
- the comment on 2: reject it with the reason `smoke reject`.

Then:

```sh
ghv drafts
ghv drafts --json
```

The issue, pull request, comment, and edit should show their URLs, the edited comment with `"edited": true`, and the last comment `rejected: smoke reject`.

## A review reply

On github.com, leave an inline review comment on a line of pull request 2. Find its ID and draft a reply:

```sh
ghv pr activity 2 -R you/ghv-smoke
printf 'Smoke test reply.\n' > /tmp/reply.md
ghv review reply 2 1234567890 -R you/ghv-smoke --body-file /tmp/reply.md
```

Use the ID `pr activity` shows in place of `1234567890`. Send it with `ghv-apply`, and check that it lands in the review thread.

## A fork

Fork a small public repository you don't own and haven't forked:

```sh
ghv fork octocat/Spoon-Knife
```

Approve it in `ghv-apply`. It should wait for GitHub and then print `Fork ready: https://github.com/you/Spoon-Knife`.

## The record

```sh
/usr/local/bin/ghv-apply log
/usr/local/bin/ghv-apply log --verify
```

`log` should list one row per send, oldest first. `log --verify` should say `Nothing made outside ghv-apply.`, though GitHub's event feed can lag by hours, so recent sends may show up under `In the audit log but not in GitHub's events`. Now make a comment by hand on github.com and run `log --verify` again later: it should list that comment under `Made outside ghv-apply` and exit 1.

## Doctor

```sh
ghv doctor --login you
/usr/local/bin/ghv-apply doctor --login you
```

Run as yourself, both should report `same-user` as HIGH and end with `Verdict: Uncontained`, exit non-zero. That's correct: the agent should run as its own user ([containment.md](containment.md)). If you've set that up, run the check at the end of that guide too.

## Clean up

- Delete `you/ghv-smoke` and the Spoon-Knife fork on github.com.
- Delete the throwaway write token at https://github.com/settings/tokens.
- `rm ~/.config/ghv/vault ~/.local/state/ghv/audit.jsonl`
- Remove the drafts: `rm -r ~/.local/share/ghv`, or for the shared queue `sudo find /var/lib/ghv -type f ! -name lock -delete`.
