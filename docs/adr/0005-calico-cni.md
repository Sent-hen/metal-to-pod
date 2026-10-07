# 0005: Calico as the CNI

**Status:** Accepted

## Context
kubeadm installs no pod network. Flannel is the simplest option but does not enforce NetworkPolicy. The platform layer needs NetworkPolicy to make "default-deny, apps opt in" enforceable instead of decorative. Cilium enforces policy too, but its eBPF data plane replaces the iptables and kube-proxy mechanics this project wants to expose.

## Decision
Install Calico from its upstream manifest, pinned by `CALICO_VERSION`, patched at install time:
- `CALICO_IPV4POOL_CIDR` = kubeadm's `--pod-network-cidr` (`10.244.0.0/16`).
- `IP_AUTODETECTION_METHOD=cidr=192.168.60.0/24`, so Calico builds tunnels over the lab NIC and not the NAT NIC.

The patch fails loudly (`die`) if the upstream manifest format changes, rather than installing a wrong network silently.

## Consequences
- NetworkPolicy is enforced, so the `default-deny-ingress` policy in `logistics` has real effect.
- Data plane is iptables + IP-in-IP, inspectable with `ip route` and `iptables-save`.
- The manifest install is simpler than the Tigera operator but harder to upgrade. The operator is the production recommendation.
- The dual-NIC autodetection issue is a classic lab failure mode; the walkthrough explains it.
