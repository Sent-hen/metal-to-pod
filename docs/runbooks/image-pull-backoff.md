# Runbook: ErrImagePull / ImagePullBackOff

## 1. Read the exact error
```bash
kubectl -n logistics describe pod <pod> | sed -n '/Events/,$p'
```

| Message contains | Meaning |
|---|---|
| `x509: certificate signed by unknown authority` | node does not trust the lab CA |
| `x509: certificate is valid for ..., not ...` | name in the image ref is not in the registry cert's SANs |
| `connection refused` / `i/o timeout` | registry down or unreachable |
| `not found` / `manifest unknown` | tag was never pushed (did the Push stage run?) |
| `no such host` | the `/etc/hosts` managed block is missing on that node |

## 2. Reproduce from the node, outside Kubernetes
```bash
vagrant ssh worker1
getent hosts registry.lab
curl -v https://registry.lab:5000/v2/ 2>&1 | grep -E 'SSL|subject|issuer|HTTP/'
sudo crictl pull registry.lab:5000/delivery-promise:<tag>     # same code path kubelet uses
cat /etc/containerd/certs.d/registry.lab:5000/hosts.toml
```

## 3. Check the registry side
```bash
vagrant ssh ci -c "docker compose -p ci ps registry; docker compose -p ci logs --tail 30 registry"
curl -s https://registry.lab:5000/v2/delivery-promise/tags/list
```

## 4. Fixes
- Trust or hosts problems: `vagrant provision <node> --provision-with common,k8s-node` (idempotent).
- Registry down: `docker compose -p ci up -d registry`. Kubelet retries with backoff (up to 5 minutes), so delete the pod to retry right away.
- Missing tag: re-run the Jenkins build.
