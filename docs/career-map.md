# Career map: what this project demonstrates, per role

The lab was built with three target roles in mind. This page maps each role to the artifacts in the repo, and to the interview questions those artifacts let you answer with evidence. It ends with a mapping to the OMSCS Computing Systems specialization.

---

## Infra / platform / architecture

| What they look for | Where it is |
|---|---|
| Infrastructure as code, one source of truth | `provision/lab.env` read by both the Vagrantfile (Ruby) and the bash provisioners |
| Idempotent, re-runnable automation | every `provision/*.sh` step checks state first (`vagrant provision` is safe) |
| Understanding the cluster, not just using it | kubeadm bootstrap, the dual-NIC `node-ip` and Calico autodetection fixes, `docs/walkthrough.md` |
| A platform contract between platform and app teams | `deploy/platform` vs `deploy/k8s/app`; RBAC makes it enforceable (ADR 0007) |
| Guardrails | Pod Security `restricted`, default-deny NetworkPolicy, ResourceQuota, LimitRange |
| Supply chain basics | private CA and TLS registry, immutable SHA tags, config validation gate |
| Written architecture decisions | `docs/adr/`: every choice with its trade-off and "what production changes" |

**Questions you can answer from this repo:**
- *Walk me through what happens when a node joins a cluster.* (bootstrap token, CA hash pinning, CSR)
- *Why not give CI cluster-admin?* (ADR 0007, and show `kubectl auth can-i`)
- *How would you make this production-grade?* The ADRs already list it: HA control plane, ephemeral CI agents, registry auth and signing, GitOps, cert-manager.

## SRE / reliability

| What they look for | Where it is |
|---|---|
| SLIs/SLOs tied to user impact | `docs/slo.md`: why 4xx is "good", why probes are excluded, why 99.5% |
| Burn-rate alerting | `deploy/monitoring/slo-rules.yml`, **unit-tested** with `promtool test rules` |
| Safe deploys | `maxUnavailable: 0`, readiness gating, progress deadline, automatic rollback, external smoke test that checks the commit |
| Graceful shutdown | SIGTERM → fail readiness → drain → `Shutdown` (`app/cmd/server/main.go`) |
| Runbooks | `docs/runbooks/`: symptom → command → meaning tables |
| Chaos / game days | `docs/drills.md`: 8 drills with written hypotheses |
| Blameless postmortems | `docs/postmortems/TEMPLATE.md` plus the ones you write from drills |
| Metric cardinality hygiene | route label = mux pattern, not raw path (tested) |

**Questions you can answer from this repo:**
- *Tell me about a time something broke.* Use a drill postmortem, with the numbers from `load.sh`.
- *How do you choose alert thresholds?* The 14.4x/6x burn rates and the reason for two windows.
- *Liveness vs readiness?* Liveness never checks dependencies; readiness fails during drain.
- *What happens to in-flight requests during a rolling update?* The endpoint propagation race and `DRAIN_SECONDS`.

## Logistics / supply-chain (technical roles)

The workload is a real supply-chain problem rather than a to-do app:

| Concept | Where it is |
|---|---|
| Delivery promise / available-to-promise date | `app/internal/promise`: order time → ship date → delivery date |
| Carrier cutoffs in DC-local time zones | same instant, different answer per DC (tested) |
| Business-day calendars and carrier holidays | `network.json`, `isBusinessDay`, `addBusinessDays` |
| Sourcing / DC selection | earliest delivery, then lowest cost, deterministic tie-break, alternatives returned for explainability |
| Zone-based rate cards, parcel vs LTL weight limit | `cost()`, `MaxWeightLb` |
| Network design as data | lanes, cutoffs and holidays change through a validated config rollout, with no code change |
| Operational KPIs | `promise_quotes_total{dc}` (DC mix), `two_day_coverage` (share of demand promisable in ≤ 2 days) |

**The story:** *"Operations wanted to move Reno's cutoff from 2 pm to 3 pm. That is a one-line change to `network.json`. The pipeline validates it with the production binary, rolls it out with zero downtime, and the DC mix and two-day coverage graphs show the effect within minutes."* Run it for real: change `cutoff_hour`, push, and watch `sum by (dc) (rate(promise_quotes_total[5m]))`.

Extensions that deepen the domain side: capacity-aware sourcing (DC daily capacity as a gauge, spill to the next-best DC), promise accuracy (record actual deliveries, alert when the on-time rate drops), and a lanes backtest with `order_time`.

---

## OMSCS: Computing Systems specialization

Course numbers and specialization rules change; check them against the current OMSCS catalog before planning. These are the courses whose material this lab uses directly:

| Course | Where you meet it in this lab |
|---|---|
| **CS 6200 Graduate Intro to OS (GIOS)** | processes, threads, IPC, and the OS mechanisms containers are built from: namespaces, cgroups, and the overlay filesystem (walkthrough §2-3). Graceful shutdown is signal handling. |
| **CS 6210 Advanced OS (AOS)** | virtualization: how VMware multiplexes CPU and memory (walkthrough §1); distributed systems foundations |
| **CS 6250 Computer Networks** | the pod overlay (IP-in-IP), routing between pod CIDR blocks, NAT (Service DNAT, NodePort), DNS, TLS |
| **CS 7210 Distributed Computing** | etcd is a Raft cluster (walkthrough §4 shows its state). Controllers are level-triggered reconcile loops. Consensus is why HA control planes need 3 nodes. |
| **CS 6211 System Design for Cloud Computing** | the closest match overall: cluster managers, containers, orchestration, and reliability at scale |
| **CS 6238 Secure Computer Systems** | PKI and trust chains (ADR 0004), least privilege (ADR 0007), sandboxing (Pod Security, seccomp, dropped capabilities) |
| **CS 6515 Graduate Algorithms** | required in the specialization; the DC-selection ranking is a small optimization problem that could grow into a real one |

**Suggested sequencing with the lab:**
1. **Before or while taking GIOS:** run the lab and do walkthrough §2-3 by hand (`/proc/<pid>/ns`, `systemd-cgls`). Namespaces and cgroups make much more sense once you have seen them hold up a real pod.
2. **With Computer Networks:** capture traffic on a worker with `tcpdump -i tunl0` and `-i any port 30080` while `load.sh` runs, and trace a NodePort request through the iptables DNAT chain to a pod on another node.
3. **With Distributed Computing or AOS:** build the HA control-plane extension (3 control-plane nodes), kill the etcd leader, and watch the re-election.
4. **With SDCC:** replace the manual pieces with production equivalents (GitOps, operators, autoscaling) and write ADRs explaining what each one costs.

For the logistics side, OMSCS lets you take some courses from outside the CS core as free electives. Operations-research courses such as simulation, optimization or analytics modeling (the ISyE courses shared with OMSA) pair well with this lab's domain. Confirm eligibility with your advisor.
