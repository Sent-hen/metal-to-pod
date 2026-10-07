#!/usr/bin/env bash
# End-to-end health check, layer by layer, bottom to top. Run on cp1:
#   vagrant ssh cp1 -c "bash /vagrant/scripts/verify.sh"
# Each check prints PASS/FAIL; the exit code is the number of failures.
set -uo pipefail
source "${REPO_DIR:-/vagrant}/provision/lab.env"
export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}"
fails=0

check() {
  local name="$1"; shift
  if out=$("$@" 2>&1); then
    printf '  \033[32mPASS\033[0m %s\n' "$name"
  else
    printf '  \033[31mFAIL\033[0m %s\n' "$name"
    printf '%s\n' "$out" | tail -n 5 | sed 's/^/         /'
    fails=$((fails + 1))
  fi
}

nodes_ready() {
  local expected=$((WORKER_COUNT + 1)) ready
  ready=$(kubectl get nodes --no-headers | awk '$2 == "Ready"' | wc -l)
  kubectl get nodes --no-headers
  [[ "$ready" -eq "$expected" ]]
}
ds_ready() {
  local ns="$1" ds="$2" want have
  want=$(kubectl -n "$ns" get ds "$ds" -o jsonpath='{.status.desiredNumberScheduled}')
  have=$(kubectl -n "$ns" get ds "$ds" -o jsonpath='{.status.numberReady}')
  echo "$ds $have/$want ready"; [[ -n "$want" && "$have" == "$want" ]]
}
deploy_ready() { kubectl -n "$1" rollout status "deployment/$2" --timeout=10s; }
pod_dns() {
  kubectl run "dns-check-$RANDOM" --rm -i --restart=Never --quiet \
    --image=busybox:1.36 -n default -- nslookup kubernetes.default.svc.cluster.local
}
http_ok() { curl -fsS --max-time 5 -o /dev/null "$1"; }
promise_ok() {
  curl -fsS --max-time 5 -X POST "http://$(worker_ip 1):$APP_NODEPORT/v1/promise" \
    -H 'Content-Type: application/json' -d '{"region":"west","service_level":"two_day","weight_lb":2}' \
    | grep -q delivery_date
}
worker_ip() { local b="${WORKER_IP_BASE%.*}" l="${WORKER_IP_BASE##*.}"; echo "$b.$((l + $1))"; }

echo "Layer 1 - hypervisor / network"
check "API server /livez on $CP_IP:6443"         curl -fsS --max-time 5 --cacert /etc/kubernetes/pki/ca.crt -o /dev/null "https://$CP_IP:6443/livez"
check "CI VM reachable ($CI_IP)"                 ping -c1 -W2 "$CI_IP"

echo "Layer 2 - cluster"
check "all $((WORKER_COUNT + 1)) nodes Ready"    nodes_ready
check "calico-node on every node"                ds_ready kube-system calico-node
check "CoreDNS available"                        deploy_ready kube-system coredns
check "pod DNS resolution"                       pod_dns

echo "Layer 3 - registry"
check "registry TLS verifies against lab CA"     http_ok "https://$REGISTRY_HOST:$REGISTRY_PORT/v2/"

echo "Layer 4 - CI"
check "Jenkins answering"                        http_ok "http://$CI_IP:$JENKINS_PORT/login"

echo "Layer 5 - platform services"
check "Prometheus ready"                         http_ok "http://$(worker_ip 1):$PROMETHEUS_NODEPORT/-/ready"

echo "Layer 6 - application"
if kubectl -n "$APP_NAMESPACE" get deployment delivery-promise &>/dev/null; then
  check "delivery-promise rollout complete"      deploy_ready "$APP_NAMESPACE" delivery-promise
  check "readiness via NodePort"                 http_ok "http://$(worker_ip 1):$APP_NODEPORT/readyz"
  check "POST /v1/promise returns a promise"     promise_ok
  echo "  serving: $(curl -fsS --max-time 5 "http://$(worker_ip 1):$APP_NODEPORT/version" 2>/dev/null)"
else
  echo "  SKIP app not deployed yet - push to the lab remote to run the pipeline (README step 4)"
fi

echo
if (( fails == 0 )); then echo "All checks passed."; else echo "$fails check(s) failed - see docs/runbooks/."; fi
exit "$fails"
