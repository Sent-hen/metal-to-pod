#!/usr/bin/env bash
# CI VM: Docker, the self-hosted git remote (bare repo + post-receive hook), the
# private registry and the Jenkins controller.
set -euo pipefail
source "${REPO_DIR:-/vagrant}/provision/lib.sh"

log "Docker engine"
apt_install docker.io docker-compose-v2 docker-buildx
systemctl enable --now docker >/dev/null
id "$LAB_USER" &>/dev/null && usermod -aG docker "$LAB_USER"
# The daemon pushes to the registry, so the daemon must trust the lab CA.
reg_certs="/etc/docker/certs.d/$REGISTRY_HOST:$REGISTRY_PORT"
mkdir -p "$reg_certs"
install -m 644 "$SHARED_DIR/pki/ca.crt" "$reg_certs/ca.crt"

log "Self-hosted git remote"
bare=/srv/git/metal-to-pod.git
install -d -m 755 -o "$LAB_USER" -g "$LAB_USER" /srv/git
if [[ ! -d "$bare" ]]; then
  sudo -u "$LAB_USER" git init --quiet --bare --initial-branch=main "$bare"
fi
install -m 755 -o "$LAB_USER" -g "$LAB_USER" "$REPO_DIR/ci/git-hooks/post-receive" "$bare/hooks/post-receive"

log "Secrets for CI services"
secrets=/opt/metal-to-pod/secrets
install -d -m 755 /opt/metal-to-pod
install -d -m 750 "$secrets"
install -m 644 "$SHARED_DIR/pki/ca.crt" "$SHARED_DIR/pki/registry.crt" "$secrets/"
# Registry runs as root in its container; Jenkins runs as uid 1000.
install -m 600 "$SHARED_DIR/pki/registry.key" "$secrets/registry.key"
echo "waiting for the deployer kubeconfig from cp1 (bring cp1 up first)"
wait_for_file "$SHARED_DIR/deployer.kubeconfig" 1200
install -m 640 -g 1000 "$SHARED_DIR/deployer.kubeconfig" "$secrets/deployer.kubeconfig"

if [[ ! -s "$SHARED_DIR/jenkins-admin-password" ]]; then
  openssl rand -base64 18 | tr -d '/+=' > "$SHARED_DIR/jenkins-admin-password"
fi
cat > /opt/metal-to-pod/ci.env <<ENV
DOCKER_GID=$(getent group docker | cut -d: -f3)
JENKINS_ADMIN_PASSWORD=$(cat "$SHARED_DIR/jenkins-admin-password")
JENKINS_URL=http://$CI_IP:$JENKINS_PORT/
K8S_MINOR=$K8S_MINOR
ENV
chmod 600 /opt/metal-to-pod/ci.env

log "Starting registry + Jenkins (first build of the Jenkins image takes a few minutes)"
docker compose --project-directory "$REPO_DIR/ci" -f "$REPO_DIR/ci/docker-compose.yml" \
  --env-file /opt/metal-to-pod/ci.env up -d --build

log "Waiting for the registry (TLS, verified against the lab CA)"
for _ in $(seq 1 30); do
  curl -fsS "https://$REGISTRY_HOST:$REGISTRY_PORT/v2/" >/dev/null 2>&1 && break
  sleep 2
done
curl -fsS "https://$REGISTRY_HOST:$REGISTRY_PORT/v2/" >/dev/null || die "registry not answering over TLS"

log "Waiting for Jenkins"
for _ in $(seq 1 90); do
  code=$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$JENKINS_PORT/login" || true)
  [[ "$code" == 200 ]] && break
  sleep 5
done
[[ "${code:-}" == 200 ]] || die "Jenkins did not come up - check: docker compose -p ci logs jenkins"

log "ci: done"
cat <<MSG

  Jenkins   http://$CI_IP:$JENKINS_PORT/   (user: admin, password: shared/jenkins-admin-password)
  Registry  https://$REGISTRY_HOST:$REGISTRY_PORT/v2/_catalog
  Git       $LAB_USER@$CI_IP:$bare   (see README step 3 to add it as a remote)

MSG
