# 0007: Pipeline deploys with a namespace-scoped ServiceAccount

**Status:** Accepted

## Context
The quickest way to let Jenkins deploy is to copy `admin.conf` into it. Then anyone who can change the Jenkinsfile, or who compromises the CI VM, owns the cluster: they can read every Secret, create privileged pods, and change RBAC.

## Decision
- `deploy/platform/deployer-rbac.yaml` creates the ServiceAccount `jenkins-deployer`, bound to a Role in `logistics` only. The Role covers Deployments, ReplicaSets, Services, ConfigMaps, PDBs, NetworkPolicies and read-only pods, logs and events. It excludes Secrets, Namespaces, RBAC, and anything cluster-scoped.
- `cluster/make-deployer-kubeconfig.sh` turns its token into a kubeconfig. Jenkins loads it from JCasC as a file credential, which is only exposed inside `withCredentials {}`.
- Namespace, quota, LimitRange and default-deny policy are owned by the platform layer and applied with admin credentials.
- The `logistics` namespace enforces Pod Security `restricted`, so even with deploy rights the pipeline cannot run a privileged or root pod.

## Consequences
- The blast radius of a pipeline compromise is one namespace's workloads.
- The app team cannot loosen its own guardrails. For example, deleting `default-deny-ingress` is denied, which you can check with `kubectl auth can-i`.
- The long-lived token is a lab simplification. Production would use short-lived tokens (TokenRequest API), or OIDC federation between CI and the cluster.
