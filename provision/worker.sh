#!/usr/bin/env bash
# Joins this node to the cluster using the command published by the control plane.
set -euo pipefail
source "${REPO_DIR:-/vagrant}/provision/lib.sh"

if [[ -s /etc/kubernetes/kubelet.conf ]]; then
  log "already joined - skipping"
  exit 0
fi

log "Waiting for join command from cp1"
wait_for_file "$SHARED_DIR/join.sh" 900

log "kubeadm join"
# join.sh is a single "kubeadm join <endpoint> --token ... --discovery-token-ca-cert-hash ..." line.
# The CA hash pins the control plane's identity: the worker will not trust an
# API server that merely answers on the right IP.
join_cmd=$(cat "$SHARED_DIR/join.sh")
eval "$join_cmd --node-name $(hostname)"

log "worker: done"
