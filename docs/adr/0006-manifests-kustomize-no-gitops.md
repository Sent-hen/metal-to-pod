# 0006: Plain manifests + kustomize; image tag via generated overlay

**Status:** Accepted

## Context
Deploy options: raw `kubectl apply`, kustomize (built into kubectl), Helm, or GitOps (Argo CD or Flux). The README's scope rules out tooling that abstracts a layer away. The pipeline also needs to set a new image tag on every build, without a bot committing "bump tag" changes to git.

## Decision
- The app's manifests in `deploy/k8s/app` are plain YAML plus a `kustomization.yaml` that:
  - declares the image's registry path (`images:`),
  - generates the config ConfigMap from `network.json` with a content-hash name suffix.
- At deploy time `scripts/ci/deploy.sh` writes a throwaway overlay (`build/overlay`) that sets `newTag: <sha>`, and applies it with `kubectl apply -k`.
- Push-based deploys from CI; no GitOps controller.

## Consequences
- No extra binaries: `kubectl` alone renders and applies.
- **Config changes roll out safely:** a new `network.json` gets a new ConfigMap name, which changes the pod template and triggers a normal rolling update with probes. Old ConfigMaps are kept so `rollout undo` can return to a ReplicaSet that references them. The cost is that stale ConfigMaps pile up. `kubectl apply --prune` or a periodic cleanup would handle that.
- The image tag lives in the cluster and in Jenkins build metadata, not in git. `kubectl rollout history` (with `change-cause`) maps revisions back to commits.
- **Drift is not detected.** A manual `kubectl set env` persists until the next pipeline run overwrites it (drill 2). Detecting and reverting drift is what GitOps adds; Argo CD is the natural v2 step.
- No Helm templating, which is fine for one app in one environment. Several environments would add kustomize overlays per environment, not Helm.
