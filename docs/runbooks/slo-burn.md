# Runbook: SLO burn (availability or latency)

**Signals:** `DeliveryPromiseErrorBudgetFastBurn` (page); `...SlowBurn` or `...LatencySLO` (ticket).

## 1. Is it real, and how bad?
In Prometheus:
```promql
delivery_promise:error_ratio:rate5m
sum by (route, code) (rate(http_requests_total{app="delivery-promise", code=~"5.."}[5m]))
sum by (pod)         (rate(http_requests_total{app="delivery-promise", code=~"5.."}[5m]))
```
- Errors on **every pod** point at the release or the config: go to step 2.
- Errors on **one pod or one node** point at infrastructure: go to step 3.

## 2. Did something change?
```bash
kubectl -n logistics rollout history deployment/delivery-promise
kubectl -n logistics get deploy delivery-promise -o jsonpath='{.spec.template.spec.containers[0].env}'
```
- A deploy in the last hour: roll back first, investigate after.
  `kubectl -n logistics rollout undo deployment/delivery-promise`
- `FAULT_ERROR_RATE` or `FAULT_LATENCY_MS` not `"0"`: a drill is running, or someone forgot to end one.
- A recent `network.json` change: check the app logs.

## 3. Isolate a bad pod or node
```bash
kubectl -n logistics get pods -o wide
kubectl -n logistics logs <pod> --since=10m | grep '"status":5'
kubectl -n logistics delete pod <pod>       # the ReplicaSet replaces it
```

## 4. Latency only
```promql
histogram_quantile(0.99, sum by (le, pod) (rate(http_request_duration_seconds_bucket{app="delivery-promise",route=~".* /v1/.*"}[5m])))
sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="logistics",container="app"}[5m]))
```
Is one pod slow, or all of them? Is CPU climbing with load? As a mitigation, add a replica with `kubectl -n logistics scale deployment/delivery-promise --replicas=4`. That creates drift, so follow up with a change in git.

## 5. Afterwards
If more than 25% of the monthly budget was burned, write a postmortem ([template](../postmortems/TEMPLATE.md)).
