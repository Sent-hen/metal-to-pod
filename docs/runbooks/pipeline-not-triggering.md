# Runbook: pushed a commit, nothing happened in Jenkins

Work from the push toward the build.

## 1. Did the hook run?
The `git push lab main` output should include lines starting with `remote: metal-to-pod:`. If it does not:
```bash
vagrant ssh ci -c "ls -l /srv/git/metal-to-pod.git/hooks/post-receive"   # must be executable
vagrant provision ci --provision-with ci                                  # reinstalls it
```
A warning `could not reach Jenkins` means the hook ran but Jenkins was down. The next scheduled poll (every 15 minutes) will catch the commit.

## 2. Did Jenkins receive the notification?
In Jenkins, open **delivery-promise** → **Git Polling Log**. It should show a poll at push time. To trigger one by hand:
```bash
vagrant ssh ci -c "curl -s 'http://localhost:8080/git/notifyCommit?url=file:///srv/git/metal-to-pod.git'"
```
- `No git jobs using repository` means the URL in the hook does not match the job's remote URL exactly.
- An HTTP 403 means the `NOTIFY_COMMIT_ACCESS_CONTROL` flag did not apply. Check `JAVA_OPTS` in `ci/docker-compose.yml`, then run `docker compose -p ci up -d --force-recreate jenkins`.

## 3. Did polling find a change?
The polling log says `No changes` when the pushed branch is not `main`, or when that commit was already built.
```bash
vagrant ssh ci -c "git --git-dir=/srv/git/metal-to-pod.git log --oneline -3 main"
```

## 4. The build started but failed at checkout
- `Checking out a local repository is not allowed` means `ALLOW_LOCAL_CHECKOUT` is missing from `JAVA_OPTS`.
- `detected dubious ownership` means the `safe.directory` line in `ci/jenkins/Dockerfile` is missing. Rebuild the image.

## 5. Jenkins configuration looks wrong or empty
JCasC re-applies `ci/jenkins/casc.yaml` at startup. Errors show up in:
```bash
vagrant ssh ci -c "docker compose -p ci logs jenkins | grep -iE 'casc|configuration-as-code|SEVERE' | tail"
```
