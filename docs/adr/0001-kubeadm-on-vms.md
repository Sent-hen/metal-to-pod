# 0001: Bootstrap Kubernetes with kubeadm on plain VMs

**Status:** Accepted

## Context
The project's goal is to understand every layer from VM to pod. Managed Kubernetes (EKS, GKE, AKS) hides the control plane completely. Single-binary distributions (k3s, k0s, microk8s) are excellent for production at the edge, but they bundle and preconfigure the runtime, CNI, datastore and certificates.

## Decision
Use upstream `kubeadm` on Ubuntu VMs: 1 control plane and N workers, containerd as the runtime, packages from `pkgs.k8s.io` pinned to one minor version (`K8S_MINOR` in `lab.env`) and held with `apt-mark hold`.

## Consequences
- Every step is visible: kernel modules, cgroup driver, static pod manifests, the bootstrap token join, and the CNI install.
- The same flow transfers to real bare metal and to any cloud VM.
- **Single control plane:** losing cp1 stops scheduling and deploys (running pods keep serving; see drill 7). HA would need 3 control-plane nodes, a load-balanced `--control-plane-endpoint`, and stacked or external etcd. That is deliberately out of scope for v1.
- Upgrades are manual (`kubeadm upgrade plan/apply`, one minor at a time). That is a good exercise, and it is listed as an extension.
- Certificates expire after 1 year (`kubeadm certs check-expiration`).
