# Reliability drills (game days)

Each drill breaks one thing on purpose. It starts with a written hypothesis: **write down what you expect before you run it**. Afterwards you compare what actually happened, and a postmortem ([template](postmortems/TEMPLATE.md)) goes wherever the two differ. These are the source for the README deliverable *"at least one thing that broke and how it was diagnosed."*

Start background load before every drill, so impact shows up as numbers and not as a feeling:

```bash
vagrant ssh ci -c "bash /vagrant/scripts/load.sh 600"    # 10 minutes; prints 2xx/4xx/5xx at the end
```

Watch in a second terminal:

```bash
vagrant ssh cp1 -c "kubectl -n logistics get pods -o wide -w"
```

and in Prometheus (`http://192.168.60.11:30090`): `delivery_promise:error_ratio:rate5m`, plus the **Alerts** page.

---

## 1. Bad release → automatic rollback

**Hypothesis:** a release whose pods never become Ready is rolled back by the pipeline, and users see **zero** errors, because `maxUnavailable: 0` means no old pod is removed until a new one is Ready.

```bash
# on the host, in the repo (Git Bash), or edit the value by hand
sed -i '/SIMULATE_BAD_RELEASE/{n;s/"false"/"true"/}' deploy/k8s/app/deployment.yaml
git diff
git commit -am "drill: ship a release that never becomes ready"
git push lab main
```

**Observe:** one extra pod stays `0/1 Running` with its readiness probe failing. After `progressDeadlineSeconds` (120 s), `rollout status` fails, `scripts/ci/deploy.sh` prints events and runs `rollout undo`, and the build goes red. `load.sh` should report 0 5xx.

**Then:** `git revert HEAD && git push lab main`. The rollback fixed the cluster but not git; until the revert, the next push would ship the same bad change again.

## 2. Error-budget burn → page

**Hypothesis:** a 50% error rate fires `DeliveryPromiseErrorBudgetFastBurn` within about 10 minutes and clears within about 5 minutes of the fix.

```bash
vagrant ssh cp1 -c "kubectl -n logistics set env deployment/delivery-promise FAULT_ERROR_RATE=0.5"
vagrant ssh ci  -c "bash /vagrant/scripts/load.sh 900"
# ...watch the alert go Pending -> Firing...
vagrant ssh cp1 -c "kubectl -n logistics set env deployment/delivery-promise FAULT_ERROR_RATE=0"
```

**Also notice:** you just changed production outside the pipeline. `kubectl -n logistics get deploy delivery-promise -o yaml` no longer matches git. The next pipeline run silently overwrites it. That is **configuration drift**, and it is one of the things GitOps tools exist to detect.

## 3. Drain a worker (planned maintenance)

**Hypothesis:** `kubectl drain` evicts pods one at a time without dropping below 2 ready replicas (PDB), and without errors.

```bash
vagrant ssh cp1 -c "kubectl drain worker2 --ignore-daemonsets --delete-emptydir-data"
vagrant ssh cp1 -c "kubectl uncordon worker2"
```

**Observe:** evictions that would break the PDB are refused and retried (`Cannot evict pod as it would violate the pod's disruption budget`). After uncordon, pods do **not** move back on their own; the scheduler only places new pods. Check `kubectl get pods -o wide`. If Prometheus was on worker2, its history is gone (emptyDir).

## 4. Registry outage

**Hypothesis:** running pods are unaffected. Restarting a pod on a node that already has the image works. A pod on a node *without* the image fails with `ImagePullBackOff`, and pushes fail safely.

```bash
vagrant ssh ci -c "docker compose -p ci stop registry"
# node WITH the image cached:
vagrant ssh cp1 -c "kubectl -n logistics delete pod -l app.kubernetes.io/name=delivery-promise --field-selector spec.nodeName=worker1 --wait=false"
# node WITHOUT it: remove the cached image on worker2, then delete a pod there
vagrant ssh worker2 -c "sudo crictl rmi \$(sudo crictl images -q registry.lab:5000/delivery-promise)"
vagrant ssh cp1 -c "kubectl -n logistics delete pod -l app.kubernetes.io/name=delivery-promise --field-selector spec.nodeName=worker2 --wait=false"
vagrant ssh cp1 -c "kubectl -n logistics get pods -o wide"      # ErrImagePull / ImagePullBackOff
vagrant ssh ci -c "docker compose -p ci start registry"         # backoff retries, then recovers
```

Runbook: [image-pull-backoff.md](runbooks/image-pull-backoff.md).

## 5. Unplanned node loss

**Hypothesis:** killing a worker causes a short burst of errors or timeouts until the node is marked NotReady and its pods leave the Service endpoints. Replacement pods are created only after the 300 s `not-ready` toleration.

```bash
vagrant halt --force worker2         # from the host: pulls the virtual power cord
```

**Measure:** the time from halt to the node reporting `NotReady`, to the pods being marked not ready, to the replacement pods being Running. Compare the 5xx/timeout count against drill 3. This drill usually produces the most interesting postmortem: *why is the default 5 minutes, and what would you tune?* (`tolerationSeconds`, node-monitor grace period, and client retries.)

```bash
vagrant up worker2
```

## 6. Latency regression

**Hypothesis:** adding 400 ms to every request fires `DeliveryPromiseLatencySLO` (ticket, not page) after about 10 minutes, with no availability alert.

```bash
vagrant ssh cp1 -c "kubectl -n logistics set env deployment/delivery-promise FAULT_LATENCY_MS=400"
# ... then set it back to 0
```

## 7. Control plane down

**Hypothesis:** the app keeps serving with cp1 powered off, because the data plane (kube-proxy rules, running containers) does not need the API server. Deploys and self-healing stop.

```bash
vagrant halt cp1
curl http://192.168.60.11:30080/version                   # still answers
# Jenkins deploys now fail at "kubectl apply" - try a push
vagrant up cp1
```

## 8. Join a node after the token expires

Join tokens last 24 h. Add a worker a day after `vagrant up` and it fails with an authentication error.

```bash
vagrant ssh cp1 -c "sudo kubeadm token list"
vagrant provision cp1 --provision-with control-plane      # re-publishes shared/join.sh
```

---

## Recording results

| Drill | Date | Expected | Actual | Postmortem? |
|---|---|---|---|---|
| 1 bad release | | 0 errors, auto-rollback | | |
| 2 budget burn | | page < 10 min | | |
| 3 drain | | 0 errors | | |
| 4 registry | | ImagePullBackOff only on cold node | | |
| 5 node loss | | short error burst | | |
| 6 latency | | ticket after ~10 min | | |
| 7 control plane | | serving continues | | |
