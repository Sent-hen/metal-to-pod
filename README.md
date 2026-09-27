# Bare Metal to Deployed Pod: A Self-Hosted CI/CD Pipeline on VMware and Kubernetes

## Overview

This project builds one continuous, self-hosted path from a virtual machine to a running application in production — no public cloud involved at any layer. A small VMware-hosted environment is provisioned into a Kubernetes cluster, and a self-hosted CI/CD pipeline builds, containerizes, and deploys a sample application automatically on every commit. The goal isn't any single tool; it's proving that the full chain — hypervisor, cluster, pipeline, container, deployment — works together as one system, and that every step in that chain is understood rather than assumed.

## Why This Matters

**For managers:** this demonstrates the ability to stand up a complete, automated deployment pipeline — from virtual infrastructure to a live application — independent of any cloud vendor. It's the same skill set that keeps infrastructure portable, auditable, and cost-controlled, and it's directly transferable to any environment, cloud or on-prem.

**For engineers:** this is deliberately not a "click deploy" exercise. Every layer — VM provisioning, cluster bootstrap, image build, registry, and deployment trigger — is built and wired by hand, so each piece can be explained, debugged, and rebuilt independently rather than treated as a black box.

## What Gets Built

```
VMware Host (ESXi / Workstation)
        │
        ▼
Provisioned VMs  ──►  Kubernetes Cluster (kubeadm: 1 control plane + 2+ workers)
        │
        ▼
Self-Hosted CI/CD (Jenkins or GitLab CI)
        │
        ▼
Docker Build  ──►  Private Container Registry
        │
        ▼
Automated Deployment to Cluster (on every commit)
```

- **Hypervisor layer** — VMs provisioned on VMware, sized and networked deliberately rather than defaulted.
- **Cluster layer** — a real `kubeadm`-bootstrapped cluster, not a managed one, so control-plane setup and node join aren't hidden.
- **CI/CD layer** — a self-hosted pipeline runner, not a SaaS pipeline, so the trigger, build, and deploy stages are all owned and visible.
- **Container layer** — application images built and pushed to a private registry as part of the pipeline, not pre-built.
- **Deployment layer** — pipeline applies manifests (or a Helm chart) to the cluster automatically on each push, closing the loop from commit to running pod.

## Scope & Approach

1. Provision the VM set on VMware.
2. Bootstrap the Kubernetes cluster with `kubeadm`.
3. Stand up a private container registry.
4. Deploy a self-hosted CI/CD runner and connect it to a source repo.
5. Wire the pipeline: commit → build image → push to registry → deploy to cluster.
6. Validate the full loop end-to-end and document it with the diagram above.

## Why This Scope, Specifically

This intentionally stays at the primitive level rather than layering on service meshes, GitOps tooling, or vendor-specific platforms. The point is to understand what Kubernetes, Docker, and CI/CD are actually doing underneath, not to accumulate tool logos. Anything that would abstract a layer away rather than expose it is out of scope for this version.

## Deliverables

- A working, demoable pipeline: a commit results in a redeployed pod, start to finish.
- A short written walkthrough of each layer, using the diagram above as the spine.
- Notes on at least one thing that broke during the build and how it was diagnosed — the habit that separates operating a system from understanding it.

## Status

This project is also a deliberate test case for Claude Code: the plan is to direct the build — provisioning scripts, pipeline configuration, and Kubernetes manifests — primarily through Claude Code, to evaluate how well an agentic coding tool can execute a full-stack infrastructure project rather than isolated snippets.