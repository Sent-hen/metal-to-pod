# Bare Metal to Deployed Pod: A Self-Hosted CI/CD Pipeline on VMware and Kubernetes

## Overview

This project builds one continuous, self-hosted path from a virtual machine to a running application in production, with no public cloud involved at any layer. A small VMware-hosted environment is provisioned into a Kubernetes cluster. A self-hosted CI/CD pipeline then builds, containerizes and deploys a sample application automatically on every commit. The goal isn't any single tool. It's proving that the full chain (hypervisor, cluster, pipeline, container, deployment) works together as one system, and that every step in that chain is understood rather than assumed.

The application is a real logistics service rather than a hello-world. **`delivery-promise`** answers the question every checkout page asks: *"if this order is placed now, which distribution center ships it, and when will it arrive?"* It handles DC-local carrier cutoffs, business-day calendars, carrier holidays, lane transit times and a zone-based rate card. It runs with production-style reliability engineering around it: SLOs, burn-rate alerts, automatic rollback, runbooks and failure drills.

## Why This Matters

**For managers:** this demonstrates the ability to stand up a complete, automated deployment pipeline, from virtual infrastructure to a live application, independent of any cloud vendor. It's the same skill set that keeps infrastructure portable, auditable and cost-controlled, and it transfers directly to any environment, cloud or on-prem.

**For engineers:** this is deliberately not a "click deploy" exercise. Every layer (VM provisioning, cluster bootstrap, image build, registry, and deployment trigger) is built and wired by hand, so each piece can be explained, debugged and rebuilt independently rather than treated as a black box.

**Target roles:** the work is shaped for infra/platform/architecture, SRE/reliability, and technical logistics/supply-chain roles. [docs/career-map.md](docs/career-map.md) maps each role to the evidence in this repo, and maps the lab to the OMSCS Computing Systems specialization.

## What Gets Built

```
Windows host ── VMware Workstation ── Vagrant                  (Vagrantfile, provision/lab.env)
        │
        ▼
4 Ubuntu 24.04 VMs on 192.168.60.0/24
   cp1 .10 │ worker1 .11 │ worker2 .12 │ ci .20                  (provision/common.sh, k8s-node.sh)
        │
        ▼
Kubernetes cluster: kubeadm, containerd, Calico                (provision/control-plane.sh, worker.sh)
   + platform layer: namespaces, Pod Security, default-deny,
     quotas, least-privilege deployer RBAC, Prometheus         (deploy/platform, deploy/monitoring)
        │
        ▼
Self-hosted CI on the ci VM                                    (provision/ci.sh, ci/)
   bare git repo + post-receive hook ──► Jenkins (config-as-code)
        │
        ▼
Docker build (tests in-image) ──► registry.lab:5000 (TLS via lab CA)   (app/Dockerfile, Jenkinsfile)
        │
        ▼
kubectl apply -k (image pinned to commit SHA) ──► rolling update       (scripts/ci/deploy.sh)
   └─ rollout fails or smoke test fails ──► automatic rollback         (scripts/ci/rollback.sh, smoke.sh)
```

- **Hypervisor layer:** VMs on VMware Workstation, sized and networked from a single file (`provision/lab.env`) that both Vagrant and the shell provisioners read.
- **Cluster layer:** a real `kubeadm` cluster, not a managed one, so control-plane setup and node join aren't hidden. The fixes that dual-NIC VMs always need (kubelet `--node-ip`, Calico interface autodetection) are explicit and commented.
- **CI/CD layer:** a self-hosted git remote whose `post-receive` hook triggers Jenkins. Jenkins is configured entirely as code: no setup wizard, no UI clicks.
- **Container layer:** a multi-stage build that runs unit tests as a gate and ships a ~10 MB distroless, non-root image, tagged with the commit SHA and pushed to a private TLS registry.
- **Deployment layer:** the pipeline deploys with a namespace-scoped ServiceAccount (not cluster-admin). It waits for the rollout, then checks from outside the cluster that the new commit is the one serving. If anything fails it rolls back.

## Scope & Approach

1. Provision the VM set on VMware. → `Vagrantfile`, `provision/common.sh`
2. Bootstrap the Kubernetes cluster with `kubeadm`. → `provision/k8s-node.sh`, `control-plane.sh`, `worker.sh`
3. Stand up a private container registry. → `ci/docker-compose.yml`, lab CA in `common.sh`
4. Deploy a self-hosted CI/CD runner and connect it to a source repo. → `provision/ci.sh`, `ci/jenkins/`, `ci/git-hooks/`
5. Wire the pipeline: commit → build image → push to registry → deploy to cluster. → `Jenkinsfile`, `scripts/ci/`
6. Validate the full loop end-to-end and document it. → `scripts/verify.sh`, [docs/walkthrough.md](docs/walkthrough.md)

## Why This Scope, Specifically

This intentionally stays at the primitive level rather than layering on service meshes, GitOps tooling or vendor-specific platforms. The point is to understand what Kubernetes, Docker and CI/CD are actually doing underneath, not to accumulate tool logos. Anything that would abstract a layer away rather than expose it is out of scope for this version. Even the app's Prometheus metrics are written against the text exposition format directly, instead of through a client library. Each simplification is recorded, together with what production would do instead, in [docs/adr/](docs/adr/).

---

# Running the project

## 0. Prerequisites (Windows host)

| Need | Notes |
|---|---|
| **16 GB RAM host**, 4+ cores, ~50 GB free disk | the default profile is sized for a 16 GB laptop (built on an ASUS ROG Zephyrus G16): cp1 2 GB, workers 2 GB each, ci 3 GB = **9 GB for VMs**. See *Host sizing* below |
| [VMware Workstation Pro](https://www.vmware.com/products/desktop-hypervisor/workstation-and-fusion) 17+ | free for personal use |
| [Vagrant](https://developer.hashicorp.com/vagrant/install) 2.4+ | |
| [Vagrant VMware Utility](https://developer.hashicorp.com/vagrant/install/vmware) | a Windows service; must be running |
| `vagrant-vmware-desktop` plugin | `vagrant plugin install vagrant-vmware-desktop` |
| Git for Windows | provides Git Bash and OpenSSH |
| Internet access from the VMs | apt, pkgs.k8s.io, Docker Hub, GitHub (Calico manifest) |
| *Optional:* Go 1.24+ | only for running the app's tests on the host |

No VMware? VirtualBox 7 also works: the Vagrantfile has a `virtualbox` provider block. Use `--provider virtualbox` below.

```powershell
vagrant plugin install vagrant-vmware-desktop
```

## 1. Bring up the lab (~20-30 min)

```powershell
git clone https://github.com/Sent-hen/metal-to-pod.git
cd metal-to-pod
vagrant up --provider vmware_desktop --no-parallel
```

`--no-parallel` is required. VMs come up in order **cp1 → worker1 → worker2 → ci**, and later VMs wait for files that earlier ones publish into `shared/` (git-ignored): the lab CA, the join command, the deployer kubeconfig and the Jenkins password.

When it finishes, the ci VM prints the Jenkins and registry URLs.

## 2. Verify every layer

```powershell
vagrant ssh cp1 -c "bash /vagrant/scripts/verify.sh"
```

Expected: every check `PASS` from Layer 1 (network) to Layer 5 (Prometheus). Layer 6 (application) is skipped until the first pipeline run. If something fails, the script points to [docs/runbooks/](docs/runbooks/).

## 3. Connect your repo to the lab's git remote

The ci VM hosts the git remote Jenkins builds from ([ADR 0003](docs/adr/0003-self-hosted-git-remote.md)). Add an SSH alias for it, then add it as a second remote next to GitHub:

```powershell
New-Item -ItemType Directory -Force $HOME\.ssh | Out-Null
vagrant ssh-config ci --host metal-lab-ci | Add-Content -Encoding ascii $HOME\.ssh\config
git remote add lab metal-lab-ci:/srv/git/metal-to-pod.git
```

## 4. First deploy: push a commit

```powershell
git push lab main
```

The push output includes `remote: metal-to-pod: Jenkins notified`. Open Jenkins at **http://192.168.60.20:8080** as user `admin`. Get the password with:

```powershell
Get-Content shared\jenkins-admin-password
```

The `delivery-promise` job runs: *Resolve version → Unit tests → Build image → Push → Config gate → Deploy → Smoke test.* The first run takes a few minutes because it pulls the base images.

## 5. Use the service

```powershell
Invoke-RestMethod -Method Post -Uri http://192.168.60.11:30080/v1/promise -ContentType application/json `
  -Body '{"region":"northeast","service_level":"ground","weight_lb":3}'
```

```json
{
  "region": "northeast", "service_level": "ground", "weight_lb": 3, "order_time": "...",
  "selected":     { "dc": "ATL", "ship_date": "2026-09-30", "delivery_date": "2026-10-02", "transit_business_days": 2, "zone": 4, "cost_usd": 11.9 },
  "alternatives": [ { "dc": "DAL", "...": "..." }, { "dc": "RNO", "...": "..." } ]
}
```

| Endpoint | Purpose |
|---|---|
| `POST /v1/promise` | body `{region, service_level, weight_lb, dc?, order_time?}` → promise + alternatives |
| `GET /v1/network` | DCs, regions and service levels currently loaded |
| `GET /version` | build version and **commit**: which release is serving |
| `GET /healthz`, `/readyz` | liveness / readiness (readiness fails while draining) |
| `GET /metrics` | Prometheus metrics (RED + business KPIs) |

Regions: `northeast southeast south midwest west northwest`. Service levels: `ground two_day overnight`.

Prometheus: **http://192.168.60.11:30090**. Query examples are in [walkthrough §10](docs/walkthrough.md#10-observability-prometheus). Run `vagrant ssh ci -c "bash /vagrant/scripts/load.sh 300"` first so there is traffic to see.

## 6. Close the loop: commit → redeployed pod

Make a business change and watch it ship. For example, move Reno's carrier cutoff from 2 pm to 3 pm:

```powershell
# edit deploy/k8s/app/network.json:  "code": "RNO" ... "cutoff_hour": 15
git commit -am "ops: move RNO carrier cutoff to 15:00"
git push lab main
vagrant ssh cp1 -c "kubectl -n logistics get pods -w"      # new pods roll in one at a time
```

The ConfigMap's content hash changes, so this triggers a normal rolling update. It needs no image change, and the config gate validates it first. `kubectl -n logistics rollout history deployment/delivery-promise` lists one revision per commit.

A code change works the same way (`app/`). `curl http://192.168.60.11:30080/version` reports the new commit once the smoke test passes.

## 7. Break it on purpose

[docs/drills.md](docs/drills.md) has eight failure drills, each with a written hypothesis: a bad release with automatic rollback, error-budget burn, a node drain under a PDB, a registry outage, node power loss, a latency regression, losing the control plane, and an expired join token. Write up at least one in [docs/postmortems/](docs/postmortems/TEMPLATE.md). That write-up is the *"thing that broke and how it was diagnosed"* deliverable.

## 8. Tear down

```powershell
vagrant destroy -f
Remove-Item -Recurse -Force shared
git remote remove lab
```

Also delete the `Host metal-lab-ci` block from `$HOME\.ssh\config`.

---

## Day-to-day commands

```powershell
vagrant status                                        # VM states
vagrant ssh cp1                                       # kubectl is configured for the vagrant user
vagrant halt                                          # pause the lab; `vagrant up` resumes it (no re-provision)
vagrant provision <vm> --provision-with <step>        # re-run one idempotent step: common, k8s-node, control-plane, worker, ci
$env:KUBECONFIG="$PWD\shared\admin.conf"; kubectl get nodes   # optional: kubectl from the host
```

Working on the app without the lab:

```powershell
cd app
go test ./...
$env:NETWORK_CONFIG="..\deploy\k8s\app\network.json"; go run ./cmd/server   # http://localhost:8080
```

Testing the alert rules (requires [promtool](https://prometheus.io/download/)):

```bash
promtool check rules deploy/monitoring/slo-rules.yml
promtool test rules deploy/monitoring/slo-rules.test.yml
```

## Host sizing

The defaults in `provision/lab.env` target a **16 GB laptop**: 9 GB for the four VMs, leaving about 7 GB for Windows. On a machine like the G16, RAM is the limit, not CPU (8 vCPUs total is light for a modern laptop chip).

Before `vagrant up` on a 16 GB host:

- **Free memory.** Close Chrome or Edge, games, and launchers. If you use WSL2 or Docker Desktop, run `wsl --shutdown`: the `vmmem` process can hold several GB.
- **Plug in and use the Performance / Turbo profile** in Armoury Crate. On battery, the laptop throttles and provisioning slows down a lot.
- **Hyper-V / Memory Integrity:** Windows 11 often has Virtualization-Based Security on. VMware Workstation 17 still runs, through the Windows Hypervisor Platform, but more slowly. That is fine for this lab; turn it off only if you understand the security trade-off.
- **Check headroom while it runs:** Task Manager → Performance → Memory. If Windows starts paging heavily, run `vagrant halt ci` while you explore the cluster, and start it again with `vagrant up ci`.

| Host RAM | Profile (`lab.env`) | VM total |
|---|---|---|
| 16 GB *(default)* | CP 2048, 2 workers × 2048, CI 3072 | 9 GB |
| 16 GB, still tight | `WORKER_COUNT=1`, CI 2560 | 6.5 GB. The drain drill gets blocked by the PDB, which is itself worth observing |
| 32 GB+ | CP 4096, 2 workers × 3072, CI 4096 | 14 GB |

Change sizing before the first `vagrant up`. To resize existing VMs, run `vagrant reload <vm>` (memory takes effect on restart). Changing `WORKER_COUNT` after the cluster exists means `vagrant destroy -f` and starting over, because every VM's `/etc/hosts` is generated from it.

## Running on ESXi or bare metal instead of Workstation

The provisioning scripts assume only Ubuntu 24.04 and a directory shared between machines. Vagrant just runs them in order. To run by hand:

1. Create the VMs with the IPs from `provision/lab.env`, and clone this repo to the same path on each (for example `/opt/metal-to-pod`).
2. Make `shared/` the same directory on every VM (an NFS export from `ci` works), or copy its files across between steps.
3. Run, as root, in this order:
   ```bash
   export REPO_DIR=/opt/metal-to-pod LAB_USER=<your user>
   ./provision/common.sh                                          # every VM
   NODE_IP=<this vm ip> ./provision/k8s-node.sh                   # cp1 and workers
   ./provision/control-plane.sh                                   # cp1
   ./provision/worker.sh                                          # each worker
   ./provision/ci.sh                                              # ci
   ```

## Troubleshooting the bring-up

| Symptom | Fix |
|---|---|
| host freezes or VMs crawl during `vagrant up` | out of RAM: see *Host sizing*. Close apps, run `wsl --shutdown`, or use the 1-worker profile |
| `kubeadm init` preflight: `the system RAM (... MB) is less than the minimum 1700 MB` | `CP_MEMORY` set below 2048 in `lab.env` |
| `vagrant up` can't find the provider | the Vagrant VMware Utility service must be running: `Get-Service vagrant-vmware-utility` |
| scripts fail with `$'\r': command not found` | CRLF line endings. `.gitattributes` forces LF; re-checkout: `git rm -r --cached . ; git reset --hard` |
| worker waits forever for `join.sh` | cp1 provisioning failed; see `vagrant provision cp1`. VMs must come up with `--no-parallel` |
| worker join fails with an auth error > 24 h after setup | token expired: `vagrant provision cp1 --provision-with control-plane`, then `vagrant provision workerN` |
| pods stuck `ContainerCreating`, nodes `NotReady` | CNI: `kubectl -n kube-system get pods -o wide \| Select-String calico` |
| Jenkins build fails at the Docker Hub pull (`toomanyrequests`) | Docker Hub anonymous rate limit; wait, or `docker login` on the ci VM |
| push to `lab` doesn't start a build | [runbooks/pipeline-not-triggering.md](docs/runbooks/pipeline-not-triggering.md) |
| rollout failed / rolled back | [runbooks/rollout-failed.md](docs/runbooks/rollout-failed.md) |
| `ImagePullBackOff` | [runbooks/image-pull-backoff.md](docs/runbooks/image-pull-backoff.md) |

## Repository layout

```
Vagrantfile                 VM definitions (reads provision/lab.env)
provision/                  lab.env (topology) + idempotent provisioning scripts per role
cluster/                    cluster admin helpers (deployer kubeconfig)
ci/                         Jenkins image, config-as-code, compose file, git hook
Jenkinsfile                 pipeline stages
scripts/ci/                 deploy / rollback / smoke: the pipeline's real logic
scripts/                    verify.sh (layer checks), load.sh (traffic)
app/                        delivery-promise (Go, stdlib only) + Dockerfile
deploy/platform/            platform-owned: namespaces, guardrails, deployer RBAC
deploy/monitoring/          Prometheus, SLO recording/alert rules + rule unit tests
deploy/k8s/app/             app-owned: Deployment, Service, PDB, NetworkPolicy, network.json
docs/                       architecture, walkthrough, SLOs, drills, runbooks, ADRs, postmortems, career map
```

## Deliverables

- [x] A working, demoable pipeline: a commit results in a redeployed pod, start to finish (steps 4-6).
- [x] A written walkthrough of each layer, using the diagram as the spine: [docs/walkthrough.md](docs/walkthrough.md), [docs/architecture.md](docs/architecture.md).
- [ ] Notes on at least one thing that broke during the build and how it was diagnosed. Run a [drill](docs/drills.md) or record a real bring-up failure in `docs/postmortems/`. This one has to come from actually running the lab.

Beyond the original scope: SLOs with tested burn-rate alerts, automatic rollback, least-privilege CI, runbooks, and ADRs.

## Extensions (v2 ideas)

Each of these has an ADR explaining why it is out of scope today:

- **HA control plane:** 3 control-plane nodes behind a VIP (kube-vip or HAProxy); kill the etcd leader during load.
- **GitOps:** Argo CD pulling `deploy/`, with drift detection (compare drill 2).
- **Registry hardening:** auth, image signing with cosign, and admission-time verification.
- **Ephemeral CI agents:** Jenkins Kubernetes agents and rootless builds instead of the docker socket.
- **Grafana + Alertmanager:** SLO dashboards and alert routing; a blackbox exporter for client-side SLIs.
- **Autoscaling:** metrics-server + HPA, driven by `load.sh`.
- **Cluster upgrade drill:** `kubeadm upgrade` one minor version with zero app errors.
- **Domain:** DC capacity limits with spillover sourcing; promise-accuracy tracking.

## Status

This project is also a deliberate test case for Claude Code. The build (provisioning scripts, pipeline configuration, Kubernetes manifests, the application and its docs) was directed primarily through Claude Code, to evaluate how well an agentic coding tool can execute a full-stack infrastructure project rather than isolated snippets.

What has been verified without a hypervisor: the Go tests pass, `promtool` checks and unit tests pass for the rules, shellcheck is clean on every script, the kustomize output renders correctly (including the pipeline overlay), and all pinned upstream versions and the Calico manifest patch were checked. **Not yet verified: a full `vagrant up` on VMware.** The first real bring-up is the next milestone, and anything it breaks is material for the postmortem deliverable.
