# 0004: Private registry with TLS from a lab CA

**Status:** Accepted

## Context
Images must come from a private registry. The common shortcut is to run the registry over plain HTTP and list it as an "insecure registry" in every node's containerd and Docker configuration. That teaches the wrong habit, and it is not how registries behave outside a lab.

## Decision
- `common.sh` creates a small CA once (`shared/pki/ca.*`) and issues a server certificate for `registry.lab` and `192.168.60.20`.
- Every VM trusts the CA: the system store, containerd's `certs.d/registry.lab:5000/`, and Docker's `certs.d/`.
- `registry:2` serves that certificate on :5000.

## Consequences
- Pulls and pushes are verified TLS. The failure modes are the real ones (unknown authority, SAN mismatch), which [image-pull-backoff.md](../runbooks/image-pull-backoff.md) covers.
- The CA private key sits in `shared/` (git-ignored) on the synced folder. That is acceptable for a lab. In production the CA key lives in an HSM or Vault, and certificates rotate automatically (cert-manager).
- **No registry authentication.** Anyone on the lab network can push. Next step: htpasswd auth on the registry, `docker login` from Jenkins using a credential, and an `imagePullSecret` in `logistics`.
- No image signing or admission policy. Extensions: cosign signatures verified by an admission controller (Kyverno or the sigstore policy-controller).
- Single registry = single point of failure for *new* pulls (drill 4).
