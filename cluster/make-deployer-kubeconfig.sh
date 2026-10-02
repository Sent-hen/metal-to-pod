#!/usr/bin/env bash
# Prints a kubeconfig for the jenkins-deployer ServiceAccount (see
# deploy/platform/deployer-rbac.yaml). This is what CI uses - NOT admin.conf.
#
# The account can manage workloads in the app namespace and nothing else: it
# cannot read Secrets, touch other namespaces, create namespaces, or change
# RBAC. A compromised pipeline therefore cannot take over the cluster.
#
# Usage (on a machine with admin access): make-deployer-kubeconfig.sh > deployer.kubeconfig
set -euo pipefail
source "${REPO_DIR:-/vagrant}/provision/lab.env"

ns="$APP_NAMESPACE"
sa=jenkins-deployer
secret=jenkins-deployer-token

# The token controller fills in the Secret asynchronously after creation.
for _ in $(seq 1 30); do
  token=$(kubectl -n "$ns" get secret "$secret" -o jsonpath='{.data.token}' 2>/dev/null | base64 -d || true)
  [[ -n "$token" ]] && break
  sleep 2
done
[[ -n "${token:-}" ]] || { echo "token for $ns/$sa never appeared" >&2; exit 1; }

ca=$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')
server=$(kubectl config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')

cat <<KUBECONFIG
apiVersion: v1
kind: Config
clusters:
- name: metal-to-pod
  cluster:
    server: $server
    certificate-authority-data: $ca
users:
- name: $sa
  user:
    token: $token
contexts:
- name: $sa@metal-to-pod
  context:
    cluster: metal-to-pod
    namespace: $ns
    user: $sa
current-context: $sa@metal-to-pod
KUBECONFIG
