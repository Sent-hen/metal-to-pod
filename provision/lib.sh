# shellcheck shell=bash
# Shared helpers for provisioning scripts. Sourced, not executed.
# Every script is safe to re-run (`vagrant provision`): each step checks
# whether its work is already done before doing it.

REPO_DIR="${REPO_DIR:-/vagrant}"
SHARED_DIR="${SHARED_DIR:-$REPO_DIR/shared}"
LAB_USER="${LAB_USER:-vagrant}"
export DEBIAN_FRONTEND=noninteractive

# shellcheck source=lab.env
source "$REPO_DIR/provision/lab.env"

log()  { printf '\n\033[1;34m[%s] %s\033[0m\n' "$(hostname)" "$*"; }
warn() { printf '\033[1;33m[%s] WARN: %s\033[0m\n' "$(hostname)" "$*" >&2; }
die()  { printf '\033[1;31m[%s] ERROR: %s\033[0m\n' "$(hostname)" "$*" >&2; exit 1; }

# worker_ip N -> IP of workerN, derived from WORKER_IP_BASE.
worker_ip() {
  local base="${WORKER_IP_BASE%.*}" last="${WORKER_IP_BASE##*.}"
  echo "$base.$((last + $1))"
}

# wait_for_file PATH TIMEOUT_SECONDS - cross-VM handoff through $SHARED_DIR.
wait_for_file() {
  local path="$1" timeout="${2:-600}" waited=0
  until [[ -s "$path" ]]; do
    (( waited >= timeout )) && die "timed out after ${timeout}s waiting for $path (did the VM that writes it provision successfully?)"
    (( waited % 30 == 0 )) && echo "waiting for $path ..."
    sleep 5; waited=$((waited + 5))
  done
}

apt_install() {
  apt-get install -y -qq --no-install-recommends "$@" >/dev/null
}
