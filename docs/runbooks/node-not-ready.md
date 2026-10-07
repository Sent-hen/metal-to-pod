# Runbook: node NotReady / replicas low

**Signals:** `kubectl get nodes` shows `NotReady`, or the alert `DeliveryPromiseReplicasLow` fired.

## 1. Scope
```bash
kubectl get nodes -o wide
kubectl -n logistics get pods -o wide        # which replicas were on that node?
kubectl describe node <node> | sed -n '/Conditions/,/Addresses/p'
```
`Ready=Unknown` with "Kubelet stopped posting node status" means the node or the network is gone. `Ready=False` with a reason means the kubelet is running but unhealthy (disk or memory pressure, CNI not ready).

## 2. Is the VM alive?
```bash
vagrant status
vagrant ssh <node> -c "systemctl status kubelet containerd --no-pager | head -30"
vagrant ssh <node> -c "sudo journalctl -u kubelet --since '15 min ago' --no-pager | tail -40"
```

| kubelet log / condition | Cause | Fix |
|---|---|---|
| `cni plugin not initialized` | calico-node not running on that node | `kubectl -n kube-system get pods -o wide` and look at calico-node |
| `running with swap on` | swap re-enabled after a reboot | `vagrant provision <node> --provision-with k8s-node` |
| `x509: certificate has expired` | kubelet client cert (1 year) expired | `sudo kubeadm certs check-expiration` on cp1 |
| `DiskPressure=True` | images or logs filled the disk | `sudo crictl rmi --prune` |

## 3. Protect the service while you debug
With only 2 workers, losing one puts every replica on the other. If the node will not come back soon, drain it so the replacements are scheduled now instead of after the 300 s toleration:
```bash
kubectl drain <node> --ignore-daemonsets --delete-emptydir-data --force
```

## 4. Afterwards
Run `kubectl uncordon <node>`. Pods do not rebalance on their own. To spread them again:
```bash
kubectl -n logistics rollout restart deployment/delivery-promise
```
