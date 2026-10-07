#!/usr/bin/env bash
# Runs on EVERY VM first: name resolution, base packages, and the lab's private
# certificate authority (so the registry can use real TLS instead of being
# whitelisted as "insecure" on every node).
set -euo pipefail
source "${REPO_DIR:-/vagrant}/provision/lib.sh"

log "APT mirror ($UBUNTU_MIRROR) and retries"
# Canonical's default archive hosts can be slow or refuse connections from some
# networks; a single dropped download otherwise fails the whole provision.
# Rewrite every *.ubuntu.com archive/security URI (deb822 on 24.04, plus the
# legacy sources.list on older boxes) to the configured mirror.
for f in /etc/apt/sources.list.d/ubuntu.sources /etc/apt/sources.list; do
  [[ -f "$f" ]] && sed -i -E "s#https?://([a-z0-9.-]+\.)?(archive|security)\.ubuntu\.com/ubuntu/?#${UBUNTU_MIRROR}/#g" "$f"
done
cat > /etc/apt/apt.conf.d/80-metal-to-pod-retries <<APT
Acquire::Retries "5";
Acquire::http::Timeout "30";
Acquire::https::Timeout "30";
APT

log "Base packages"
apt-get update -qq
apt_install ca-certificates curl gnupg openssl jq git

log "Lab name resolution (/etc/hosts managed block)"
# No DNS server in the lab: a managed block in /etc/hosts is enough and is
# trivially inspectable. Real environments would use DNS.
sed -i '/# BEGIN metal-to-pod/,/# END metal-to-pod/d' /etc/hosts
{
  echo "# BEGIN metal-to-pod"
  echo "$CP_IP cp1"
  for i in $(seq 1 "$WORKER_COUNT"); do echo "$(worker_ip "$i") worker$i"; done
  echo "$CI_IP ci $REGISTRY_HOST jenkins.lab"
  echo "# END metal-to-pod"
} >> /etc/hosts
# Vagrant maps the hostname to 127.0.1.1; kubelet and etcd must never pick that.
sed -i "/^127\.0\.1\.1\s\+$(hostname)\b/d" /etc/hosts

log "Lab PKI"
pki="$SHARED_DIR/pki"
mkdir -p "$pki"
if [[ ! -s "$pki/ca.crt" ]]; then
  echo "generating lab CA"
  openssl req -x509 -newkey rsa:4096 -nodes -sha256 -days 825 \
    -keyout "$pki/ca.key" -out "$pki/ca.crt" \
    -subj "/CN=metal-to-pod lab CA" \
    -addext "basicConstraints=critical,CA:TRUE" \
    -addext "keyUsage=critical,keyCertSign,cRLSign" 2>/dev/null
fi
if [[ ! -s "$pki/registry.crt" ]]; then
  echo "issuing registry certificate for $REGISTRY_HOST"
  openssl req -newkey rsa:2048 -nodes -sha256 \
    -keyout "$pki/registry.key" -out "$pki/registry.csr" -subj "/CN=$REGISTRY_HOST" 2>/dev/null
  openssl x509 -req -sha256 -days 397 -in "$pki/registry.csr" \
    -CA "$pki/ca.crt" -CAkey "$pki/ca.key" -CAcreateserial -out "$pki/registry.crt" \
    -extfile <(printf 'subjectAltName=DNS:%s,IP:%s\nextendedKeyUsage=serverAuth\nkeyUsage=critical,digitalSignature,keyEncipherment\n' "$REGISTRY_HOST" "$CI_IP") 2>/dev/null
fi
# Trust the CA system-wide (curl, containerd and docker all honour this).
install -m 644 "$pki/ca.crt" /usr/local/share/ca-certificates/metal-to-pod-ca.crt
update-ca-certificates >/dev/null

log "common: done"
