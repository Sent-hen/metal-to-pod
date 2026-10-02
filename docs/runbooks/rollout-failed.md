# Runbook: rollout failed / pipeline rolled back

**Signals:** the Jenkins "Deploy" or "Smoke test" stage is red with `!!! ROLLING BACK`, or the alert `DeliveryPromiseMixedVersions` fired.

## 1. Confirm user impact first
```bash
kubectl -n logistics get deploy delivery-promise          # READY should be 3/3 after the rollback
curl -s http://192.168.60.11:30080/version                  # which commit is serving?
```
If READY is 3/3 on the previous commit, **the incident is over for users**. What remains is a bad commit on `main`.

## 2. Find out why the new pods were not Ready
The Jenkins log already contains `get pods` and recent events from `deploy.sh`. Otherwise:
```bash
kubectl -n logistics rollout history deployment/delivery-promise
kubectl -n logistics get rs -l app.kubernetes.io/name=delivery-promise     # the newest RS has 0 ready
kubectl -n logistics describe pod <new-pod>        # Events: probe failures, pull errors, OOMKilled
kubectl -n logistics logs <new-pod> --previous     # if it crash-looped
```

| Symptom in `describe` | Likely cause | Next |
|---|---|---|
| `Readiness probe failed: ... statuscode: 503` | app not ready: config, `SIMULATE_BAD_RELEASE` | app logs |
| `ErrImagePull` / `ImagePullBackOff` | registry, tag, or TLS | [image-pull-backoff.md](image-pull-backoff.md) |
| `CrashLoopBackOff`, logs show `load network config` | bad `network.json` got past the gate | fix config |
| `OOMKilled` | memory limit (128Mi) exceeded | profile before raising it |
| `violates PodSecurity "restricted"` | manifest weakened the securityContext | restore it |
| `exceeded quota` | surge pod does not fit the ResourceQuota | platform team |

## 3. Fix forward or revert
Revert unless the fix is obvious and small:
```bash
git revert <sha> && git push lab main
```

## 4. If the rollback itself failed
`rollout undo` needs a previous revision; the very first deploy has none. Redeploy a known-good commit through the pipeline instead:
```bash
git push lab <good-sha>:main --force-with-lease    # rewrites the lab remote only, not GitHub
```
