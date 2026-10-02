#!/usr/bin/env bash
# Bootstraps the control plane with kubeadm, installs the CNI, applies the
# platform layer, and publishes what the other VMs need into $SHARED_DIR:
#   join.sh               - worker join command (token valid 24h)
#   admin.conf            - cluster-admin kubeconfig (for you, not for CI)
#   deployer.kubeconfig   - least-privilege kubeconfig for the pipeline
set -euo pipefail
source "${REPO_DIR:-/vagrant}/provision/lib.sh"
export KUBECONFIG=/etc/kubernetes/admin.conf

if [[ ! -s /etc/kubernetes/admin.conf ]]; then
  log "kubeadm init"
  kubeadm init \
    --apiserver-advertise-address="$CP_IP" \
    --control-plane-endpoint="$CP_IP:6443" \
    --pod-network-cidr="$POD_CIDR" \
    --service-cidr="$SERVICE_CIDR" \
    --node-name="$(hostname)"
else
  log "control plane already initialised - skipping kubeadm init"
fi

log "kubeconfig for $LAB_USER and the shared dir"
if id "$LAB_USER" &>/dev/null; then
  home=$(getent passwd "$LAB_USER" | cut -d: -f6)
  install -d -o "$LAB_USER" -g "$LAB_USER" "$home/.kube"
  install -m 600 -o "$LAB_USER" -g "$LAB_USER" /etc/kubernetes/admin.conf "$home/.kube/config"
fi
install -m 600 /etc/kubernetes/admin.conf "$SHARED_DIR/admin.conf"

log "Calico CNI $CALICO_VERSION"
if ! kubectl -n kube-system get daemonset calico-node &>/dev/null; then
  curl -fsSL -o /tmp/calico.yaml \
    "https://raw.githubusercontent.com/projectcalico/calico/$CALICO_VERSION/manifests/calico.yaml"
  # The stock manifest leaves the pool CIDR commented out (defaulting to
  # 192.168.0.0/16, which would collide with nothing here but would NOT match
  # kubeadm's --pod-network-cidr). Uncomment it with our value, and tell
  # calico-node which NIC to use: autodetection picks the first interface, which
  # on these VMs is the NAT adapter.
  awk -v pod_cidr="$POD_CIDR" -v node_cidr="$NODE_CIDR" '
    /# - name: CALICO_IPV4POOL_CIDR/ {
      match($0, /^ */); ind = substr($0, 1, RLENGTH)
      print ind "- name: CALICO_IPV4POOL_CIDR"
      print ind "  value: \"" pod_cidr "\""
      print ind "- name: IP_AUTODETECTION_METHOD"
      print ind "  value: \"cidr=" node_cidr "\""
      getline   # drop the commented-out default value line
      next
    }
    { print }' /tmp/calico.yaml > /tmp/calico-lab.yaml
  grep -q "value: \"cidr=$NODE_CIDR\"" /tmp/calico-lab.yaml \
    || die "failed to patch calico.yaml - did the upstream manifest format change?"
  kubectl apply -f /tmp/calico-lab.yaml
fi

log "Waiting for the control-plane node to become Ready"
kubectl wait --for=condition=Ready "node/$(hostname)" --timeout=600s

log "Publishing worker join command"
kubeadm token create --ttl 24h --print-join-command > "$SHARED_DIR/join.sh"

log "Platform layer (namespaces, guardrails, RBAC, monitoring)"
kubectl apply -k "$REPO_DIR/deploy/platform"

log "Least-privilege kubeconfig for the pipeline"
bash "$REPO_DIR/cluster/make-deployer-kubeconfig.sh" > "$SHARED_DIR/deployer.kubeconfig.tmp"
mv "$SHARED_DIR/deployer.kubeconfig.tmp" "$SHARED_DIR/deployer.kubeconfig"

log "control-plane: done"
kubectl get nodes -o wide
