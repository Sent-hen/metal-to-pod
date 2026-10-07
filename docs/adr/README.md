# Architecture Decision Records

Short records of decisions that shaped the lab: the context, the choice, and the costs accepted. Format follows Michael Nygard's ADR template. Each ADR is immutable once accepted; to change a decision, add a new ADR that supersedes it.

| # | Decision | Status |
|---|---|---|
| [0001](0001-kubeadm-on-vms.md) | Bootstrap Kubernetes with kubeadm on plain VMs | Accepted |
| [0002](0002-jenkins-self-hosted-ci.md) | Jenkins, configured as code, as the self-hosted CI | Accepted |
| [0003](0003-self-hosted-git-remote.md) | Self-hosted bare git remote with a post-receive trigger | Accepted |
| [0004](0004-registry-tls-private-ca.md) | Private registry with TLS from a lab CA | Accepted |
| [0005](0005-calico-cni.md) | Calico as the CNI | Accepted |
| [0006](0006-manifests-kustomize-no-gitops.md) | Plain manifests + kustomize; image tag via generated overlay | Accepted |
| [0007](0007-least-privilege-deployer.md) | Pipeline deploys with a namespace-scoped ServiceAccount | Accepted |
