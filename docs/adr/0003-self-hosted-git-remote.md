# 0003: Self-hosted bare git remote with a post-receive trigger

**Status:** Accepted

## Context
The pipeline must run on every commit, with no public cloud involved in any layer. GitHub webhooks cannot reach a host-only lab network without a tunnel. Polling GitHub works, but it adds up to a poll interval of latency and makes a SaaS part of the deploy path. A full git server (Gitea or GitLab) adds another stateful service to operate.

## Decision
The `ci` VM hosts a bare repository at `/srv/git/metal-to-pod.git`, reached over SSH as a second remote (`lab`). Its `post-receive` hook calls Jenkins' `notifyCommit` endpoint, and Jenkins polls the repo, which is bind-mounted read-only into the container. A 15-minute scheduled poll backs up a missed notification.

GitHub (`origin`) stays the place where the code is published. `lab` is the deployment source of truth.

## Consequences
- Commit-to-build latency is about 1 second, with no inbound firewall holes and no tokens.
- This is how git hosting works underneath: a git server is a bare repo, SSH, and hooks.
- `notifyCommit` is opened to unauthenticated callers **for polling only** (`disabled-for-polling`). A caller can make Jenkins check for changes, but cannot trigger a build of an arbitrary SHA.
- Jenkins must be allowed to check out a local path (`ALLOW_LOCAL_CHECKOUT=true`). That is harmless here because the path is a read-only mount of a repo we control.
- No pull requests, reviews or branch protection on `lab`. Those happen on GitHub before pushing to `lab`.
