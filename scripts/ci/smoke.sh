#!/usr/bin/env bash
# Post-deploy verification from OUTSIDE the cluster, through the same NodePort a
# client would use. Proves two things the rollout status alone does not:
#   1. the commit we just built is what is actually answering (not a stale pod)
#   2. the business endpoint works end-to-end, not just the health check
set -euo pipefail
: "${SMOKE_URL:?}" "${IMAGE_TAG:?}"

echo "--- waiting for $SMOKE_URL/version to report commit $IMAGE_TAG ---"
seen=0
for _ in $(seq 1 30); do
  body=$(curl -fsS --max-time 3 "$SMOKE_URL/version" || true)
  if [[ "$body" == *"\"commit\":\"$IMAGE_TAG\""* ]]; then
    seen=$((seen + 1))
    # Several consecutive hits: kube-proxy spreads requests across pods, so a
    # single match could be one new pod among old ones.
    (( seen >= 5 )) && break
  else
    seen=0
    echo "  got: ${body:-<no response>}"
  fi
  sleep 2
done
(( seen >= 5 )) || { echo "!!! new version never consistently served"; exit 1; }
echo "ok: commit $IMAGE_TAG is serving"

echo "--- functional check: POST /v1/promise ---"
resp=$(curl -fsS --max-time 5 -X POST "$SMOKE_URL/v1/promise" \
  -H 'Content-Type: application/json' \
  -d '{"region":"south","service_level":"ground","weight_lb":4}')
echo "$resp"
[[ "$resp" == *'"delivery_date"'* ]] || { echo "!!! promise response missing delivery_date"; exit 1; }

echo "--- negative check: invalid request is a 4xx, not a 5xx ---"
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -X POST "$SMOKE_URL/v1/promise" \
  -H 'Content-Type: application/json' -d '{"region":"mars","service_level":"ground","weight_lb":4}')
[[ "$code" == 422 ]] || { echo "!!! expected 422 for unknown region, got $code"; exit 1; }

echo "smoke test passed"
