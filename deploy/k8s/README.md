# chiller — Kubernetes manifests (production topology)

Single-region, multi-AZ (Q11=C). Same image, role selected per Deployment
(Q1=C). Reflects the Infrastructure Design (`aidlc-docs/construction/platform/
infrastructure-design/`):

- **manager** — `manager.yaml`: Deployment `replicas: 1` (Q3=A, resident control
  plane: API + FSM + fold). Serves `/healthz` (deep, Mongo+Valkey ping), `/livez`,
  and the read-only dashboard `/ui`.
- **worker** — `worker.yaml`: Deployment + **HPA** scaling on queue depth (Q2=B).
  Resident pool consuming the single shared Valkey stream (consumer group
  `workers`). `minReplicas/maxReplicas` bound the pool (RESILIENCY-09).
- **proxy** — `proxy.yaml`: Deployment `replicas: 1` — the **singleton** auth
  session (financial mode only; single-session invariant). Not per-scan.
- **mongo** — `mongo.yaml`: StatefulSet `replicas: 3` across AZs (Q6=B,
  RESILIENCY-08); majority write-concern (Q12=B). TLS + auth required
  (SECURITY-01/09).
- **valkey** — `valkey-queue.yaml` (durable, AOF, `noeviction`) and
  `valkey-cache.yaml` (volatile, LRU) — separate instances (Q9=C).
- **secrets** — `secret.example.yaml`: Mongo URI, Valkey addr/password, API token,
  storageState. Sourced from the deploy env, **never committed** (SECURITY-12).
- **network** — `networkpolicy.yaml`: deny-by-default; only chiller pods reach
  Mongo/Valkey; only the ingress reaches the manager API (SECURITY-07).

## Image pinning (SECURITY-10/13)
Replace every `image:` tag with a digest (`@sha256:...`) before production. No
`latest`. A CI step builds, scans (`govulncheck`), and emits an SBOM.

## Rollout (Q11=C)
Rolling update + version-pinned rollback (`kubectl rollout undo`). No canary /
blue-green (batch scanner; no live-traffic cutover needed). manager/proxy are
`replicas: 1` singletons → brief control-plane gap on restart is accepted
(in-flight scans are durable and resume; deploy-architecture §3).

> These manifests are a documented reference skeleton for the deferred Infra
> rollout, kept minimal and dependency-free. Resource requests/limits, PDBs,
> topology-spread constraints, and the concrete HPA metric source (KEDA
> ScaledObject on Valkey stream length vs. custom-metrics XLEN) are finalized
> with the chosen cluster + fleet in the Operations phase.
