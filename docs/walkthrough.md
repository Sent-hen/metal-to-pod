# Layer-by-layer walkthrough

For each layer this covers what it does, what is really happening underneath, and the commands that let you *see* it. Run the commands on `cp1` (`vagrant ssh cp1`) unless noted.

---

## 1. Hypervisor: VMware + Vagrant

**What:** four Ubuntu VMs on a host-only network, sized from `provision/lab.env`.

**Underneath:** VMware gives every VM virtual CPUs (scheduled as host threads, using VT-x/AMD-V hardware virtualization), guest-physical memory backed by host memory (extended page tables), and virtual NICs attached to a `vmnet` switch. Each VM has **two NICs**: a NAT adapter Vagrant uses for SSH and internet access, and the lab NIC. Most of the node-IP plumbing later on exists because of that second NIC.

```bash
ip -br addr            # two interfaces: NAT + 192.168.60.x
cat /etc/hosts         # the managed block written by common.sh
```

**Try:** `vagrant halt worker2` from the host, then watch `kubectl get nodes -w`.

## 2. Node OS: kernel prerequisites

**What:** `k8s-node.sh` turns off swap, loads `overlay` and `br_netfilter`, and enables IP forwarding.

**Underneath:** a container is an ordinary Linux process in its own **namespaces** (pid, net, mnt, uts, ipc, user), limited by **cgroups**. `overlay` stacks read-only image layers under a writable layer. `br_netfilter` sends traffic crossing a Linux bridge through iptables. kube-proxy's Service rules and NetworkPolicy enforcement rely on that.

```bash
lsmod | grep -E 'overlay|br_netfilter'
sysctl net.ipv4.ip_forward
# find an app container's process and look at its namespaces:
sudo crictl ps --name app -q | head -1 | xargs sudo crictl inspect | jq .info.pid
sudo ls -l /proc/<pid>/ns
```

## 3. Container runtime: containerd

**What:** kubelet talks to containerd over the CRI socket. containerd pulls images, unpacks their layers, and starts containers through `runc`.

**Underneath:** `SystemdCgroup = true` makes containerd and kubelet agree on one cgroup manager. `/etc/containerd/certs.d/registry.lab:5000/hosts.toml` tells containerd how to reach the private registry and which CA to trust.

```bash
sudo crictl images | grep delivery-promise     # (on a worker) images pulled for the app
sudo crictl pods                               # pod sandboxes (the "pause" container holds the netns)
cat /etc/containerd/certs.d/registry.lab:5000/hosts.toml
systemd-cgls -u kubepods.slice | head -30      # the cgroup tree kubelet manages
```

## 4. Cluster bootstrap: kubeadm

**What:** `kubeadm init` on cp1 and `kubeadm join` on the workers.

**Underneath:** `kubeadm init` creates a CA and certificates under `/etc/kubernetes/pki`. It writes **static pod** manifests for etcd, kube-apiserver, kube-controller-manager and kube-scheduler into `/etc/kubernetes/manifests`, and kubelet starts those itself, with no API server needed yet. Joining uses a bootstrap token plus the CA's public key hash, so a worker authenticates the control plane before trusting it. Kubelet then gets its own client certificate through a CertificateSigningRequest.

```bash
ls /etc/kubernetes/manifests                 # the control plane is just 4 static pods
sudo openssl x509 -in /etc/kubernetes/pki/apiserver.crt -noout -text | grep -A1 'Subject Alternative'
kubectl get csr                              # kubelet client certs (approved at join)
kubectl -n kube-system exec etcd-cp1 -- etcdctl \
  --cacert /etc/kubernetes/pki/etcd/ca.crt --cert /etc/kubernetes/pki/etcd/server.crt \
  --key /etc/kubernetes/pki/etcd/server.key endpoint status -w table   # Raft state
```

## 5. Pod network: Calico

**What:** every pod gets an IP from `10.244.0.0/16`, reachable from every other pod on every node.

**Underneath:** Calico's IPAM gives each node a block (for example a `/26`). On each node, routes send a remote block's traffic through an IP-in-IP tunnel to the node that owns it. The `IP_AUTODETECTION_METHOD=cidr=...` patch in `control-plane.sh` stops Calico from building those tunnels over the NAT NIC. **NetworkPolicy** is enforced by Calico as iptables rules on each node.

```bash
ip route | grep -E 'tunl0|blackhole'         # routes to other nodes' pod blocks
kubectl get pods -A -o wide                  # pod IPs by node
sudo iptables-save | grep -c cali-           # policy rules Calico programmed
```

## 6. Services and DNS: kube-proxy + CoreDNS

**What:** `delivery-promise` has a stable ClusterIP and DNS name, and a NodePort (30080) on every node.

**Underneath:** a Service is not a process. kube-proxy watches EndpointSlices and writes iptables DNAT rules that pick a Ready pod at random. A pod that fails readiness is removed from the EndpointSlice, so it gets no new connections. That removal is what graceful drain depends on.

```bash
kubectl -n logistics get endpointslices -l kubernetes.io/service-name=delivery-promise -o wide
sudo iptables -t nat -L KUBE-SERVICES -n | grep 30080
kubectl run -it --rm dns --image=busybox:1.36 --restart=Never -- nslookup delivery-promise.logistics
```

## 7. Registry: registry.lab:5000

**What:** a `registry:2` container on the ci VM, serving TLS with a certificate from the lab CA.

**Underneath:** an image is a manifest (JSON) pointing at content-addressed layer blobs (sha256). A push uploads any blobs the registry lacks and then the manifest. A pull does the reverse. Tags are mutable pointers to manifests, which is why the pipeline tags by commit SHA.

```bash
curl -s https://registry.lab:5000/v2/_catalog
curl -s https://registry.lab:5000/v2/delivery-promise/tags/list
curl -s -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json' \
  https://registry.lab:5000/v2/delivery-promise/manifests/<tag> | jq .
```

## 8. CI/CD: git hook → Jenkins → cluster

**What:** `git push lab main` triggers the Jenkinsfile stages (see [architecture.md](architecture.md#one-commit-end-to-end)).

**Underneath:** the bare repo's `post-receive` hook calls Jenkins' `notifyCommit`, and Jenkins polls the repo and finds the new SHA. The build uses Docker on the ci VM through its socket. Deploys use a ServiceAccount token whose Role only covers the `logistics` namespace.

```bash
# on ci:
cat /srv/git/metal-to-pod.git/hooks/post-receive
docker compose -p ci ps
docker compose -p ci logs --tail 50 jenkins
# prove the pipeline credential is least-privilege (on cp1):
kubectl auth can-i --list --as system:serviceaccount:logistics:jenkins-deployer -n logistics
kubectl auth can-i get secrets --as system:serviceaccount:logistics:jenkins-deployer -n logistics   # no
kubectl auth can-i create deployments --as system:serviceaccount:logistics:jenkins-deployer -n default # no
```

## 9. Workload: delivery-promise

**What:** 3 replicas behind a NodePort, with probes, a disruption budget, restricted pod security, and Prometheus scraping.

```bash
kubectl -n logistics rollout history deployment/delivery-promise   # each revision = a commit
kubectl -n logistics describe pdb delivery-promise
kubectl -n logistics get events --sort-by=.lastTimestamp | tail
curl -s http://192.168.60.11:30080/metrics | grep -E '^(http_requests_total|promise_)'
```

## 10. Observability: Prometheus

Open `http://192.168.60.11:30090`. Then:

- **Status → Targets**: three `delivery-promise` pods, plus cAdvisor on every node.
- **Alerts**: the SLO burn-rate rules from `deploy/monitoring/slo-rules.yml`.
- Queries worth graphing:
  - `delivery_promise:error_ratio:rate5m`
  - `histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket{app="delivery-promise",route=~".* /v1/.*"}[5m])))`
  - `sum by (dc) (rate(promise_quotes_total[5m]))`: which DC is winning the orders
  - `delivery_promise:two_day_coverage:ratio1h`: the business KPI
  - `sum by (pod) (rate(container_cpu_usage_seconds_total{namespace="logistics",container="app"}[5m]))`
