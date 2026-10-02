# Architecture

## System view

```
 Windows host ─ VMware Workstation ─ Vagrant
 │
 │  lab network 192.168.60.0/24 (host-only)
 │
 ├── cp1      .10  control plane: etcd, kube-apiserver, scheduler, controller-manager
 │                 (tainted NoSchedule: no app workloads land here)
 ├── worker1  .11  kubelet + containerd  ─┐  app replicas spread across workers
 ├── worker2  .12  kubelet + containerd  ─┘  (topologySpreadConstraints); Prometheus, CoreDNS
 └── ci       .20  Docker: registry.lab:5000 (TLS)  +  Jenkins :8080  +  bare git repo
```

| Range | Purpose |
|---|---|
| `192.168.60.0/24` | Node network (every VM, API server, NodePorts) |
| `10.244.0.0/16` | Pod network (Calico IPAM, IP-in-IP between nodes) |
| `10.96.0.0/12` | Service ClusterIPs (kube-proxy iptables rules) |
| `:30080` | delivery-promise NodePort, on every node |
| `:30090` | Prometheus NodePort |

All of it is declared once in [`provision/lab.env`](../provision/lab.env).

## One commit, end to end

```
git push lab main
  │  (ssh to ci VM, writes to /srv/git/metal-to-pod.git)
  ▼
post-receive hook ── GET /git/notifyCommit ──► Jenkins polls repo, sees new SHA
                                                  │
  ┌───────────────────────────────────────────────┘
  ▼  Jenkinsfile
  1 Resolve version   IMAGE_TAG = short SHA
  2 Unit tests        docker build --target test        (go vet + go test)
  3 Build image       docker build → registry.lab:5000/delivery-promise:<sha>
  4 Push              docker push  (TLS, lab CA)
  5 Config gate       new binary validates deploy/k8s/app/network.json
  6 Deploy            kubectl apply -k <overlay pinning :sha>   (deployer SA)
                      kubectl rollout status  ──fail──► rollout undo, build FAILS
  7 Smoke test        /version == <sha> via NodePort, POST /v1/promise works
                                                    ──fail──► rollout undo
```

During step 6 the Deployment surges one new pod at a time (`maxSurge: 1`, `maxUnavailable: 0`). Each node's kubelet asks containerd for the image; containerd pulls from `registry.lab:5000`, checking the certificate against the lab CA. The pod joins the Service's endpoints only once `/readyz` passes. Old pods receive SIGTERM, fail readiness, keep serving for `DRAIN_SECONDS` while kube-proxy rules converge, then exit.

## Ownership boundaries

| Layer | Owned by | Applied by | Credential |
|---|---|---|---|
| VMs, OS, containerd, kubelet | infra | Vagrant + `provision/*.sh` | root on VMs |
| Cluster (kubeadm, CNI) | platform | `control-plane.sh` | `admin.conf` |
| Platform (`deploy/platform`, `deploy/monitoring`) | platform | `control-plane.sh`, admins | `admin.conf` |
| App (`deploy/k8s/app`) | app team | Jenkins | `jenkins-deployer` SA, namespace `logistics` only |

The app team's pipeline **cannot** change namespaces, quotas, default-deny policy, RBAC or monitoring. It also cannot read Secrets. That is the platform contract, enforced by RBAC rather than by convention.

## Trust chain

1. **Lab CA** (`shared/pki/ca.crt`, made by `common.sh`) is installed into every VM's system trust store, into containerd's `certs.d`, and into Docker's `certs.d`.
2. The **registry** presents a certificate from that CA with SANs `registry.lab` and `192.168.60.20`. No node has an insecure-registry exception.
3. **Kubernetes' own CA** (made by kubeadm) is separate. Workers pin it with `--discovery-token-ca-cert-hash` when they join.
4. **Jenkins** reaches the API server with a ServiceAccount token and verifies the server against the cluster CA embedded in its kubeconfig.

## Failure domains

| Lose... | Effect | Recovery |
|---|---|---|
| one worker | ≤1 app replica lost; PDB keeps ≥2 during drains | pods reschedule after ~5 min (default `not-ready` toleration) |
| cp1 | running pods keep serving; no scheduling, no deploys, no self-healing | single control plane, so restore the VM (HA control plane is an extension) |
| ci VM | no builds, **no new image pulls**; running pods unaffected | images already cached on nodes keep working; restart VM |
| Prometheus pod | metrics gap (emptyDir) | reschedules automatically |

The registry row matters: a node that has never pulled the current image cannot start a replica while `ci` is down. See [drills.md](drills.md#4-registry-outage).

## Decisions

Design decisions and their trade-offs are recorded in [adr/](adr/).
