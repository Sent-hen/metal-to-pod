#!/usr/bin/env bash
# Steady synthetic traffic so the SLO graphs have something to show.
# Mix: mostly valid quotes across regions/service levels, ~5% invalid requests
# (4xx - these must NOT count against the availability SLO).
#
#   vagrant ssh ci -c "bash /vagrant/scripts/load.sh 300"     # 300 seconds
set -euo pipefail
source "${REPO_DIR:-/vagrant}/provision/lab.env"
duration="${1:-120}"
concurrency="${CONCURRENCY:-4}"
base="${WORKER_IP_BASE%.*}.$(( ${WORKER_IP_BASE##*.} + 1 ))"
url="${URL:-http://$base:$APP_NODEPORT}"

regions=(northeast southeast south midwest west northwest)
levels=(ground ground ground two_day overnight)

worker() {
  local end=$((SECONDS + duration)) ok=0 client_err=0 server_err=0 code body
  while (( SECONDS < end )); do
    if (( RANDOM % 20 == 0 )); then
      body='{"region":"atlantis","service_level":"ground","weight_lb":3}'
    else
      body=$(printf '{"region":"%s","service_level":"%s","weight_lb":%d}' \
        "${regions[RANDOM % ${#regions[@]}]}" "${levels[RANDOM % ${#levels[@]}]}" $((RANDOM % 40 + 1)))
    fi
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST "$url/v1/promise" \
      -H 'Content-Type: application/json' -d "$body" || echo 000)
    case "$code" in
      2??) ok=$((ok + 1)) ;;
      4??) client_err=$((client_err + 1)) ;;
      *)   server_err=$((server_err + 1)) ;;
    esac
  done
  echo "$ok $client_err $server_err"
}

echo "load: $concurrency workers x ${duration}s against $url"
results=$(for _ in $(seq 1 "$concurrency"); do worker & done; wait)
awk '{ok+=$1; c+=$2; s+=$3} END {
  t = ok + c + s
  printf "requests=%d  2xx=%d  4xx=%d  5xx/timeouts=%d  availability(excl. 4xx)=%.3f%%\n",
         t, ok, c, s, (ok + s) ? 100 * ok / (ok + s) : 0 }' <<<"$results"
