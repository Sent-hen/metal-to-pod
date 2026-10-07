#!/usr/bin/env bash
# Deploys $IMAGE_TAG to the cluster and waits for the rollout to finish.
# If the rollout does not complete (pods never become Ready, crash-loop, image
# cannot be pulled...), prints diagnostics and rolls back automatically.
#
# Requires: KUBECONFIG (deployer), IMAGE_TAG. Run from the repo root.
set -euo pipefail
: "${IMAGE_TAG:?}"
NAMESPACE="${NAMESPACE:-logistics}"
REGISTRY="${REGISTRY:-registry.lab:5000}"
IMAGE_NAME="${IMAGE_NAME:-delivery-promise}"
ROLLOUT_TIMEOUT="${ROLLOUT_TIMEOUT:-180s}"
deploy="deployment/delivery-promise"

# Generated overlay: base manifests + this commit's image tag. Keeping the tag
# out of git means the repo never needs a "bump image" commit, and the overlay
# is thrown away after use.
overlay="build/overlay"
rm -rf "$overlay" && mkdir -p "$overlay"
cat > "$overlay/kustomization.yaml" <<YAML
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../deploy/k8s/app
images:
  # Matches the name AFTER the base's own images transformer ran, so only
  # the tag changes here; the registry path stays declared in the base.
  - name: $REGISTRY/$IMAGE_NAME
    newTag: "$IMAGE_TAG"
YAML

echo "--- rendered manifests (image lines) ---"
kubectl kustomize "$overlay" | grep -E '^\s+image:' || true

echo "--- apply ---"
kubectl apply -k "$overlay"
# Recorded in `kubectl rollout history` so every revision maps back to a commit.
kubectl -n "$NAMESPACE" annotate "$deploy" --overwrite \
  kubernetes.io/change-cause="commit $IMAGE_TAG (jenkins build ${BUILD_NUMBER:-manual})"

echo "--- waiting for rollout (timeout $ROLLOUT_TIMEOUT) ---"
if kubectl -n "$NAMESPACE" rollout status "$deploy" --timeout="$ROLLOUT_TIMEOUT"; then
  kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=delivery-promise -o wide
  exit 0
fi

echo "!!! rollout of $IMAGE_TAG did not complete - diagnostics follow"
kubectl -n "$NAMESPACE" get pods -l app.kubernetes.io/name=delivery-promise -o wide || true
kubectl -n "$NAMESPACE" get events --sort-by=.lastTimestamp | tail -n 25 || true
bash "$(dirname "$0")/rollback.sh" "rollout of $IMAGE_TAG did not become ready within $ROLLOUT_TIMEOUT"
exit 1
