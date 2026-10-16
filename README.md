# metal-to-pod

A self-hosted CI/CD pipeline on VMware and Kubernetes, from bare VMs to a running pod. No public cloud is involved anywhere.

Four Ubuntu VMs on VMware Workstation become a kubeadm cluster plus a CI box. When I push a commit to the lab's git remote, Jenkins tests the app, builds an image, pushes it to a private registry and rolls it out to the cluster. If the rollout or the smoke test fails, it rolls back by itself.

I wanted to see the whole chain work end to end, and to understand each layer well enough to fix it when it breaks. So there's no managed Kubernetes, no hosted CI and no GitOps tool. Every piece is set up by scripts in this repo.

The app being deployed is `delivery-promise`, a small Go service. It answers the question a checkout page asks: if this order is placed now, which warehouse ships it, and when will it arrive? It knows about each warehouse's carrier cutoff, business days, carrier holidays, transit times per lane and a zone-based rate card. I gave it SLOs, burn-rate alerts, runbooks and failure drills, so there's something real to operate.

## What gets built

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

- **VMs:** sized and networked from one file, `provision/lab.env`, which Vagrant and the shell provisioners both read.
- **Cluster:** plain `kubeadm`, so you can see the control-plane setup and the node join. Dual-NIC VMs always need two fixes (kubelet `--node-ip` and Calico interface autodetection). Both are spelled out in the scripts with comments.
- **CI:** a bare git repo on the ci VM. Its `post-receive` hook triggers Jenkins, which is configured entirely from code. There's no setup wizard to click through.
- **Image:** a multi-stage build that runs the unit tests first, then ships a ~10 MB distroless image running as non-root. It's tagged with the commit SHA and pushed to a private registry over TLS.
- **Deploy:** the pipeline uses a ServiceAccount scoped to one namespace (not cluster-admin). It waits for the rollout, then checks from outside the cluster that the new commit is the one answering. Any failure triggers a rollback.

## Where each step lives

1. Provision the VMs on VMware: `Vagrantfile`, `provision/common.sh`
2. Bootstrap Kubernetes with `kubeadm`: `provision/k8s-node.sh`, `control-plane.sh`, `worker.sh`
3. Private container registry: `ci/docker-compose.yml`, with the lab CA set up in `common.sh`
4. CI server wired to the git remote: `provision/ci.sh`, `ci/jenkins/`, `ci/git-hooks/`
5. The pipeline itself (commit, build, push, deploy): `Jenkinsfile`, `scripts/ci/`
6. End-to-end checks and the written walkthrough: `scripts/verify.sh`, [docs/walkthrough.md](docs/walkthrough.md)

## Why so few tools

I kept this at the primitive level on purpose. There's no service mesh, no Argo CD and no vendor platform, because each of those would hide a layer I wanted to see. Even the app's Prometheus metrics are written in the text exposition format by hand, without a client library. Every shortcut I took, and what I'd do in production instead, is written up as an ADR in [docs/adr/](docs/adr/).

---

# Running it

## 0. Prerequisites (Windows host)

| Need | Notes |
|---|---|
| **16 GB RAM host**, 4+ cores, ~50 GB free disk | the default profile fits a 16 GB laptop (I built it on an ASUS ROG Zephyrus G16): cp1 2 GB, workers 2 GB each, ci 3 GB = **9 GB for VMs**. See *Host sizing* below |
| [VMware Workstation Pro](https://www.vmware.com/products/desktop-hypervisor/workstation-and-fusion) 17+ | free for personal use |
| [Vagrant](https://developer.hashicorp.com/vagrant/install) 2.4+ | |
| [Vagrant VMware Utility](https://developer.hashicorp.com/vagrant/install/vmware) | a Windows service; must be running |
| `vagrant-vmware-desktop` plugin | `vagrant plugin install vagrant-vmware-desktop` |
| Git for Windows | provides Git Bash and OpenSSH |
| Internet access from the VMs | apt, pkgs.k8s.io, Docker Hub, GitHub (Calico manifest) |
| *Optional:* Go 1.24+ | only for running the app's tests on the host |

No VMware? VirtualBox 7 works too. The Vagrantfile has a `virtualbox` provider block, so use `--provider virtualbox` below.

```powershell
vagrant plugin install vagrant-vmware-desktop
```

## 1. Bring up the lab (~20-30 min)

```powershell
git clone https://github.com/Sent-hen/metal-to-pod.git
cd metal-to-pod
vagrant up --provider vmware_desktop --no-parallel
```

You need `--no-parallel`. The VMs come up in order (**cp1, worker1, worker2, ci**), and the later ones wait on files the earlier ones write into `shared/` (git-ignored): the lab CA, the join command, the deployer kubeconfig and the Jenkins password.

When it finishes, the ci VM prints the Jenkins and registry URLs.

## 2. Check every layer

```powershell
vagrant ssh cp1 -c "bash /vagrant/scripts/verify.sh"
```

You should see `PASS` for every check from Layer 1 (network) to Layer 5 (Prometheus). Layer 6 (the app) is skipped until the pipeline has run once. If something fails, the script points you to the right file in [docs/runbooks/](docs/runbooks/).

## 3. Add the lab's git remote

Jenkins builds from a git remote on the ci VM ([ADR 0003](docs/adr/0003-self-hosted-git-remote.md)). Add an SSH alias for it, then add it as a second remote next to GitHub:

```powershell
New-Item -ItemType Directory -Force $HOME\.ssh | Out-Null
vagrant ssh-config ci --host metal-lab-ci | Add-Content -Encoding ascii $HOME\.ssh\config
git remote add lab metal-lab-ci:/srv/git/metal-to-pod.git
```

## 4. First deploy: push a commit

```powershell
git push lab main
```

The push output includes `remote: metal-to-pod: Jenkins notified`. Jenkins is at **http://192.168.60.20:8080**, user `admin`. The password is in:

```powershell
Get-Content shared\jenkins-admin-password
```

The `delivery-promise` job runs *Resolve version → Unit tests → Build image → Push → Config gate → Deploy → Smoke test*. The first run takes a few minutes while it pulls base images.

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
| `GET /version` | build version and **commit**, i.e. which release is serving |
| `GET /healthz`, `/readyz` | liveness / readiness (readiness fails while draining) |
| `GET /metrics` | Prometheus metrics (RED + business KPIs) |

Regions: `northeast southeast south midwest west northwest`. Service levels: `ground two_day overnight`.

Prometheus is at **http://192.168.60.11:30090**. Some example queries are in [walkthrough §10](docs/walkthrough.md#10-observability-prometheus). Run `vagrant ssh ci -c "bash /vagrant/scripts/load.sh 300"` first so there's traffic to look at.

## 6. Commit to redeployed pod

Make a business change and watch it ship. For example, move Reno's carrier cutoff from 2 pm to 3 pm:

```powershell
# edit deploy/k8s/app/network.json:  "code": "RNO" ... "cutoff_hour": 15
git commit -am "ops: move RNO carrier cutoff to 15:00"
git push lab main
vagrant ssh cp1 -c "kubectl -n logistics get pods -w"      # new pods roll in one at a time
```

The ConfigMap's content hash changes, so this is a normal rolling update with no new image. The config gate checks the file first. `kubectl -n logistics rollout history deployment/delivery-promise` shows one revision per commit.

Code changes in `app/` go through the same way. Once the smoke test passes, `curl http://192.168.60.11:30080/version` reports the new commit.

## 7. Break it on purpose

[docs/drills.md](docs/drills.md) has eight failure drills, each with a hypothesis written down first:

- a bad release that rolls back automatically
- error-budget burn
- draining a node under a PDB
- a registry outage
- a node losing power
- a latency regression
- losing the control plane
- an expired join token

Write-ups go in [docs/postmortems/](docs/postmortems/TEMPLATE.md).

## 8. Tear down

```powershell
vagrant destroy -f
Remove-Item -Recurse -Force shared
git remote remove lab
```

Then delete the `Host metal-lab-ci` block from `$HOME\.ssh\config`.

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

Testing the alert rules (needs [promtool](https://prometheus.io/download/)):

```bash
promtool check rules deploy/monitoring/slo-rules.yml
promtool test rules deploy/monitoring/slo-rules.test.yml
```

## Host sizing

The defaults in `provision/lab.env` assume a **16 GB laptop**: 9 GB for the four VMs, which leaves about 7 GB for Windows. On a machine like the G16, RAM runs out long before CPU does (8 vCPUs total is light for a current laptop chip).

Before `vagrant up` on a 16 GB host:

- **Free up memory.** Close the browser, games and launchers. If you use WSL2 or Docker Desktop, run `wsl --shutdown`, since the `vmmem` process can sit on several GB.
- **Plug in and pick the Performance / Turbo profile** in Armoury Crate. On battery the laptop throttles, and provisioning gets a lot slower.
- **Hyper-V / Memory Integrity:** Windows 11 often has Virtualization-Based Security turned on. VMware Workstation 17 still runs (through the Windows Hypervisor Platform), just more slowly. That's fine for this lab. Only turn it off if you understand the security trade-off.
- **Watch memory while it runs:** Task Manager → Performance → Memory. If Windows starts paging hard, `vagrant halt ci` while you poke at the cluster, then `vagrant up ci` to bring it back.

| Host RAM | Profile (`lab.env`) | VM total |
|---|---|---|
| 16 GB *(default)* | CP 2048, 2 workers × 2048, CI 3072 | 9 GB |
| 16 GB, still tight | `WORKER_COUNT=1`, CI 2560 | 6.5 GB. The PDB blocks the drain drill, which is interesting to see in itself |
| 32 GB+ | CP 4096, 2 workers × 3072, CI 4096 | 14 GB |

Change the sizing before the first `vagrant up`. To resize existing VMs, run `vagrant reload <vm>` (memory changes apply on restart). Changing `WORKER_COUNT` after the cluster exists means `vagrant destroy -f` and starting over, because every VM's `/etc/hosts` is generated from it.

## Running on ESXi or bare metal

The provisioning scripts only assume Ubuntu 24.04 and a directory shared between the machines. Vagrant just runs them in order. To do it by hand:

1. Create the VMs with the IPs from `provision/lab.env`, and clone this repo to the same path on each one (for example `/opt/metal-to-pod`).
2. Make `shared/` the same directory on every VM (an NFS export from `ci` works), or copy its files across between steps.
3. As root, run these in order:
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
| `Failed to open TCP connection to 127.0.0.1:9922` | the Vagrant VMware Utility service is stopped: `Get-Service VagrantVMware`. Its log (`C:\ProgramData\hashicorp\vagrant-vmware-desktop\logs\utility.log`) says why |
| utility log: `failed to generate VMware installation information` | Workstation 26 doesn't write the 32-bit registry value the utility reads. In an **admin** PowerShell: `New-ItemProperty -Path "HKLM:\SOFTWARE\WOW6432Node\VMware, Inc." -Name Core -Value "VMware Workstation" -PropertyType String -Force`, then `Start-Service VagrantVMware`. Reinstalling VMware can remove it again |
| `Vagrant failed to create a new VMware networking device` | create the lab network by hand once: Virtual Network Editor → Change Settings → Add Network **VMnet2**, Host-only, host adapter on, DHCP **off**, subnet `192.168.60.0` / `255.255.255.0` → Apply. Vagrant reuses it |
| apt: `Failed to fetch http://us.archive.ubuntu.com/... Connection failed` | the VMs can't reach the Ubuntu archive. Set `UBUNTU_MIRROR` in `lab.env` to a mirror that works for you, then `vagrant provision <vm>` |
| scripts fail with `$'\r': command not found` | CRLF line endings. `.gitattributes` forces LF; re-checkout with `git rm -r --cached . ; git reset --hard` |
| worker waits forever for `join.sh` | cp1 provisioning failed; see `vagrant provision cp1`. VMs must come up with `--no-parallel` |
| worker join fails with an auth error > 24 h after setup | token expired: `vagrant provision cp1 --provision-with control-plane`, then `vagrant provision workerN` |
| pods stuck `ContainerCreating`, nodes `NotReady` | CNI: `kubectl -n kube-system get pods -o wide \| Select-String calico` |
| Jenkins build fails at the Docker Hub pull (`toomanyrequests`) | Docker Hub's anonymous rate limit. Wait, or `docker login` on the ci VM |
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
docs/                       architecture, walkthrough, SLOs, drills, runbooks, ADRs, postmortems
```

## Status

- [x] Pushing a commit ends with a redeployed pod (steps 4-6).
- [x] A walkthrough of each layer: [docs/walkthrough.md](docs/walkthrough.md), [docs/architecture.md](docs/architecture.md).
- [ ] A postmortem of something that actually broke. That has to come from running the lab for real, either a drill or a bring-up failure.

I used Claude Code a lot while building this, mostly to draft the provisioning scripts, manifests and docs, which I then ran, read through and fixed.

Checked so far without a hypervisor: the Go tests pass, `promtool` checks and rule unit tests pass, shellcheck is clean on every script, and the kustomize output (including the pipeline overlay) renders correctly. I also checked every pinned upstream version and the Calico manifest patch. **I haven't done a full `vagrant up` on VMware yet.** That's next, and whatever it breaks will be the first postmortem.

## Ideas for later

Each of these has an ADR explaining why it's out of scope for now.

- **HA control plane:** 3 control-plane nodes behind a VIP (kube-vip or HAProxy), then kill the etcd leader under load.
- **GitOps:** Argo CD pulling `deploy/`, with drift detection (compare drill 2).
- **Registry hardening:** auth, image signing with cosign, and checking signatures at admission.
- **Ephemeral CI agents:** Jenkins Kubernetes agents and rootless builds instead of the docker socket.
- **Grafana + Alertmanager:** SLO dashboards and alert routing, plus a blackbox exporter for client-side SLIs.
- **Autoscaling:** metrics-server + HPA, driven by `load.sh`.
- **Upgrade drill:** `kubeadm upgrade` one minor version with zero app errors.
- **Domain:** warehouse capacity limits with spillover, and tracking how accurate the promises turn out to be.
