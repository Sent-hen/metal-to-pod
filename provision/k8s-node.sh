#!/usr/bin/env bash
# Runs on every Kubernetes node (control plane and workers): kernel prerequisites,
# containerd, and the kubeadm/kubelet/kubectl packages. Does not create or join
# a cluster - see control-plane.sh and worker.sh for that.
#
# Usage: NODE_IP=<this node's lab IP> k8s-node.sh
set -euo pipefail
source "${REPO_DIR:-/vagrant}/provision/lib.sh"
: "${NODE_IP:?set NODE_IP to the lab network IP of this node}"

log "Kernel prerequisites"
# kubelet refuses to run with swap on (by default): memory limits and eviction
# decisions assume RAM is RAM.
swapoff -a
sed -i -E '/\sswap\s/ s/^([^#])/#\1/' /etc/fstab

# overlay: containerd's snapshotter. br_netfilter: lets iptables see bridged
# pod traffic, which is how Services (kube-proxy) and NetworkPolicy work.
cat > /etc/modules-load.d/k8s.conf <<MODS
overlay
br_netfilter
MODS
modprobe overlay
modprobe br_netfilter
cat > /etc/sysctl.d/99-kubernetes.conf <<SYSCTL
net.bridge.bridge-nf-call-iptables  = 1
net.bridge.bridge-nf-call-ip6tables = 1
net.ipv4.ip_forward                 = 1
SYSCTL
sysctl --system >/dev/null

log "containerd"
apt_install containerd
mkdir -p /etc/containerd
if [[ ! -s /etc/containerd/config.toml ]] || ! grep -q 'SystemdCgroup = true' /etc/containerd/config.toml; then
  containerd config default > /etc/containerd/config.toml
  # kubelet (via kubeadm) uses the systemd cgroup driver; the runtime must match
  # or pods get killed under memory pressure in confusing ways.
  sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml
  # containerd 1.7 ships with per-registry host config disabled; point it at
  # certs.d (containerd 2.x already defaults to this, so the sed is a no-op).
  sed -i 's|config_path = ""|config_path = "/etc/containerd/certs.d"|' /etc/containerd/config.toml
fi
reg_dir="/etc/containerd/certs.d/$REGISTRY_HOST:$REGISTRY_PORT"
mkdir -p "$reg_dir"
install -m 644 "$SHARED_DIR/pki/ca.crt" "$reg_dir/ca.crt"
cat > "$reg_dir/hosts.toml" <<TOML
server = "https://$REGISTRY_HOST:$REGISTRY_PORT"

[host."https://$REGISTRY_HOST:$REGISTRY_PORT"]
  capabilities = ["pull", "resolve"]
  ca = "$reg_dir/ca.crt"
TOML
systemctl restart containerd
systemctl enable containerd >/dev/null

cat > /etc/crictl.yaml <<CRICTL
runtime-endpoint: unix:///run/containerd/containerd.sock
image-endpoint: unix:///run/containerd/containerd.sock
CRICTL

log "kubeadm / kubelet / kubectl ($K8S_MINOR)"
install -d -m 755 /etc/apt/keyrings
if [[ ! -s /etc/apt/keyrings/kubernetes-apt-keyring.gpg ]]; then
  curl -fsSL "https://pkgs.k8s.io/core:/stable:/$K8S_MINOR/deb/Release.key" \
    | gpg --batch --yes --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
fi
echo "deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/$K8S_MINOR/deb/ /" \
  > /etc/apt/sources.list.d/kubernetes.list
apt-get update -qq
apt_install kubelet kubeadm kubectl
# Pin: an unattended apt upgrade must never skew kubelet past the control plane.
apt-mark hold kubelet kubeadm kubectl >/dev/null

# VMs have two NICs (Vagrant's NAT + the lab network). Without this kubelet may
# register the NAT address, and cross-node pod traffic silently breaks.
echo "KUBELET_EXTRA_ARGS=--node-ip=$NODE_IP" > /etc/default/kubelet
systemctl enable kubelet >/dev/null

log "k8s-node: done ($(kubeadm version -o short))"
