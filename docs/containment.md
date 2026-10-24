# Running the agent as its own user

ghv keeps the write token away from the agent. That only holds if the agent can't reach into your session some other way: read `ghv-apply`'s memory, type into your terminal, read your clipboard, or use your SSH key. A process running as you can do all of that. So the agent runs as a separate Unix user, and drafts reach you through a shared queue that root sets up once.

This guide covers Linux and macOS. A sandbox that runs as your own uid (a container with your home mounted, for example) isn't supported in v0.x: `ghv doctor` can't tell it apart from your own session and reports `same-user`.

Commands below use `you` for your user name and `ghv-agent` for the agent's.

## 1. Install the binaries root-owned

`ghv-apply` must be a file the agent can't replace. Install both binaries into `/usr/local/bin`, owned by root:

```sh
sudo install -o root -m 755 ghv ghv-apply /usr/local/bin/
```

Always run `ghv-apply` by that path, or make sure `/usr/local/bin` comes before any directory the agent can write to in your `PATH`. On an Intel Mac with Homebrew, `/usr/local/bin` may belong to you rather than root; `ghv-apply doctor` reports that as a HIGH `ghv-apply` finding. Apple Silicon Homebrew uses `/opt/homebrew` and leaves `/usr/local/bin` alone.

## 2. Make the agent's user and the group

Both users go in one group, `ghv` here.

Linux:

```sh
sudo groupadd ghv
sudo useradd -m ghv-agent
sudo usermod -aG ghv you
sudo usermod -aG ghv ghv-agent
```

macOS:

```sh
sudo dseditgroup -o create ghv
sudo sysadminctl -addUser ghv-agent -password -
sudo dseditgroup -o edit -a you -t user ghv
sudo dseditgroup -o edit -a ghv-agent -t user ghv
```

Log out and back in so your own membership takes effect. Until then `ghv drafts` says `cannot read the shared queue at /var/lib/ghv: permission denied; is you in group ghv? Log out and in after adding them`.

## 3. Make the shared queue

Both `ghv` and `ghv-apply` use `/var/lib/ghv` whenever it exists. The root directory belongs to root and nobody else can write it, so neither user can swap a state directory or the lock for something else. Inside the four state directories both users can create files, and the setgid bit gives each new file the group.

Linux:

```sh
sudo install -d -o root -g root -m 755 /var/lib/ghv
sudo install -d -o root -g ghv -m 2770 /var/lib/ghv/queue /var/lib/ghv/unknown /var/lib/ghv/applied /var/lib/ghv/rejected
sudo install -o root -g ghv -m 660 /dev/null /var/lib/ghv/lock
```

macOS has no group called root, so the top directory uses wheel:

```sh
sudo install -d -o root -g wheel -m 755 /var/lib/ghv
sudo install -d -o root -g ghv -m 2770 /var/lib/ghv/queue /var/lib/ghv/unknown /var/lib/ghv/applied /var/lib/ghv/rejected
sudo install -o root -g ghv -m 660 /dev/null /var/lib/ghv/lock
```

Both binaries check this layout every time and refuse a queue that's wrong, with the command that fixes it. They never fall back to a queue in your home, where the agent's drafts would vanish from view. For example, after `sudo chmod 775 /var/lib/ghv`:

```
ghv: the shared queue at /var/lib/ghv is not safe to use: /var/lib/ghv can be changed by users other than root; sudo chmod 755 /var/lib/ghv
```

Your vault and audit log stay in your home. The agent's read token stays in its home.

## 4. Give the agent a read token, and nothing else

As `ghv-agent`, put a classic personal access token with no scopes at `~/.config/ghv/read-token`, mode 600. Make it at https://github.com/settings/tokens/new with every box unticked. `ghv token` should then report `read-only: a classic token with no scopes can only read public data`.

Don't give the agent's user an SSH key, a git credential helper, `gh`, or any other GitHub token. It doesn't need them: ghv's `pr checkout` fetches over anonymous https, and you do the pushing (see [workflow.md](workflow.md)).

Then start your agent from a shell as that user, in a checkout it owns:

```sh
sudo -iu ghv-agent
```

## 5. Lock down your own side

- **ptrace.** On Linux, set `kernel.yama.ptrace_scope` to 1 or higher: `echo kernel.yama.ptrace_scope=1 | sudo tee /etc/sysctl.d/90-ptrace-scope.conf` and `sudo sysctl --system`. Another user can't ptrace your processes anyway, but this also covers anything else running as you.
- **SSH keys.** Load your keys with `ssh-add -c`, so every use asks you first. The agent's user can't reach your agent socket, but a prompt per use is cheap insurance.
- **Your home.** Your vault (`~/.config/ghv/vault`) and audit log (`~/.local/state/ghv/audit.jsonl`) must not be writable by the agent's user. The default home modes on Linux and macOS are fine.

## 6. Check it

Run doctor as the agent's user, from the root-owned binary, so the agent can't fake the answer:

```sh
sudo -u ghv-agent /usr/local/bin/ghv-apply doctor --human-user you --login your-github-login
```

You want `Verdict: Contained (no HIGH findings).` at the end, and an INFO `queue` finding that says `drafts go to the shared queue at /var/lib/ghv (group ghv)`.

Every HIGH finding comes with the command that fixes it.

Then check the queue end to end. As `ghv-agent`, draft a comment on any public issue or pull request; nothing gets sent, since you reject it:

```sh
printf 'Checking the shared queue; this will be rejected.\n' > /tmp/check.md
ghv comment 1 -R cli/cli --body-file /tmp/check.md
```

As yourself, run `ghv-apply`, answer `r`, and type a reason, such as `checking the queue`. Then as `ghv-agent`:

```sh
ghv drafts
```

The draft should show as `rejected: checking the queue`. That proves both directions work: you read the agent's draft, and the agent reads your answer.

## Undoing it

Linux:

```sh
sudo rm -rf /var/lib/ghv
sudo userdel -r ghv-agent
sudo gpasswd -d you ghv
sudo groupdel ghv
```

macOS:

```sh
sudo rm -rf /var/lib/ghv
sudo sysadminctl -deleteUser ghv-agent
sudo dseditgroup -o delete ghv
```

On Linux, `userdel` can fail right after you used `sudo -u ghv-agent`, while that user's systemd manager is still running. Wait a minute and run it again.

Without `/var/lib/ghv`, both binaries go back to the queue in your home, for running the agent as yourself (Uncontained).
