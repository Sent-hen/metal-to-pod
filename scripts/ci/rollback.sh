#!/usr/bin/env bash
# Returns the Deployment to its previous revision and waits for it to settle.
# Usage: rollback.sh "<reason>"
set -euo pipefail
NAMESPACE="${NAMESPACE:-logistics}"
deploy="deployment/delivery-promise"
reason="${1:-unspecified}"

echo "!!! ROLLING BACK: $reason"
kubectl -n "$NAMESPACE" rollout history "$deploy" || true

if ! kubectl -n "$NAMESPACE" rollout undo "$deploy"; then
  # First-ever deploy: there is no previous revision to return to.
  echo "!!! no previous revision to roll back to - leaving the failed rollout for inspection"
  exit 1
fi
kubectl -n "$NAMESPACE" rollout status "$deploy" --timeout=180s
echo "rollback complete. The bad commit is still on main: revert or fix it, then push again."
