# Service Level Objectives: delivery-promise

## Who depends on this service and how

Checkout calls `POST /v1/promise` to show "Arrives Thursday" before the customer pays. If the call fails, checkout either shows no date, which lowers conversion, or a generic fallback that is often wrong, which leads to missed promises and support tickets. A slow call adds directly to checkout latency. That is why the SLOs cover both **availability** and **latency** of the `/v1` endpoints, measured where the service answers.

## SLIs

| SLI | Good event | Valid event | Source |
|---|---|---|---|
| Availability | response code is not 5xx | any request to a `/v1/*` route | `http_requests_total` |
| Latency | completed in < 300 ms | any request to a `/v1/*` route | `http_request_duration_seconds` bucket `le="0.3"` |

What is excluded, and why:
- **4xx responses count as good.** The service did its job by rejecting a bad request. Counting them would let one misbehaving client burn the budget.
- **`/healthz`, `/readyz` and `/metrics` are excluded.** Kubelet and Prometheus generate high-volume, trivially successful traffic there, which would dilute real errors. The rules test in `deploy/monitoring/slo-rules.test.yml` checks this exclusion.

Known gap: the SLIs are measured **server-side**. Requests that never reach a pod (every replica down, or kube-proxy misrouting) do not show up in the error ratio. The `DeliveryPromiseReplicasLow` alert and the pipeline's external smoke test partly cover that. A synthetic prober (blackbox exporter) would close it; see the extensions in the README.

## Objectives

| SLO | Target | Window | Error budget |
|---|---|---|---|
| Availability | 99.5% | 30 days rolling | 0.5% of requests, about 3.6 hours of total outage |
| Latency | 99% < 300 ms | 30 days rolling | 1% of requests may be slower |

99.5% rather than 99.9% because this is a single-control-plane lab with one registry and no redundant CI. The target should match what the architecture can deliver, and it can be raised as the architecture improves. A 99.9% promise on this topology would get spent by the first registry reboot.

## Alerting (multi-window, multi-burn-rate)

| Alert | Condition | Meaning | Severity |
|---|---|---|---|
| FastBurn | error ratio > 14.4 × 0.5% over **1h** *and* **5m** | 2% of the monthly budget per hour; gone in ~2 days | page |
| SlowBurn | error ratio > 6 × 0.5% over **6h** *and* **30m** | budget gone in ~5 days | ticket |
| LatencySLO | < 99% under 300 ms over 30m, for 10m | latency objective at risk | ticket |
| ReplicasLow | < 2 replicas scraped, for 5m | one more failure is an outage | page |
| MixedVersions | > 1 commit serving, for 15m | rollout stuck half-way | ticket |

The long window keeps the alert from firing on short blips. The short window makes it clear quickly after a fix, so nobody is paged about a problem that has already been resolved.

## Error budget policy

- **Budget remaining:** ship freely. The pipeline's automatic rollback is the safety net.
- **Budget exhausted (30-day availability < 99.5%):** feature changes pause and only reliability fixes ship until the ratio recovers. For this lab, that means working through [drills.md](drills.md) findings before adding features.
- **A single incident uses more than 25% of the budget:** it gets a written postmortem ([template](postmortems/TEMPLATE.md)).

## Queries

```promql
# 30-day availability (needs 30 days of retention; the lab keeps 3d, so use [3d] for a demo)
1 - (sum(increase(http_requests_total{app="delivery-promise",route=~".* /v1/.*",code=~"5.."}[3d]))
     / sum(increase(http_requests_total{app="delivery-promise",route=~".* /v1/.*"}[3d])))

# Budget remaining, as a fraction of the budget
1 - ((1 - <availability above>) / 0.005)
```
