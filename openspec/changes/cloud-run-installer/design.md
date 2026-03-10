## Context

Signal ngn tenants need to self-host a trader instance in their own GCP project. The platform already has a working production deployment pattern (`cloudbuild-prod.yaml`, `scripts/setup-static-ip.sh`) that serves as the blueprint. The installer must adapt that pattern for arbitrary tenant GCP projects with a guided, interactive experience — no Terraform, no CI/CD system, just `gcloud` + `bash`.

The trader image is already published to Signal ngn's Artifact Registry. Tenants do not compile code; they pull and run the image. Their runtime identity in the Signal ngn platform is their API key (issued via spot-canvas-app) and their NATS credentials file (also issued via spot-canvas-app).

## Goals / Non-Goals

**Goals:**
- Single interactive shell script (`install-tenant.sh`) that takes a fresh GCP project (billing enabled) from zero to a running Cloud Run trader
- Idempotent where possible — re-running skips resources that already exist
- Static egress IP via Cloud NAT so the tenant can whitelist their IP with Binance (and other brokers)
- All secrets stored in Secret Manager, never in env vars or files on disk
- A lighter `deploy-tenant.sh` script for subsequent upgrades/reconfigs
- Human-readable `docs/tenant-install.md` covering prerequisites and post-install operations

**Non-Goals:**
- Multi-region or HA deployments
- Terraform / Pulumi / IaC-as-code approach
- Automated CI/CD pipeline for tenant deployments
- Live trading mode enabled by default (script deploys in `paper` mode; tenant enables live manually)
- Managing broker (Binance) account setup

## Decisions

### 1. Single `install-tenant.sh` script rather than separate steps

**Decision**: One script, sequential steps, with clear section banners.

**Rationale**: Tenants should not need to orchestrate multiple scripts in the right order. A single guided script reduces error surface. Each step checks if the resource already exists and skips if so — making it safe to re-run on failure.

**Alternative considered**: Separate scripts per concern (IAM, DB, secrets, deploy) — rejected because it places orchestration burden on the tenant.

---

### 2. Pull pre-built image from Signal ngn Artifact Registry

**Decision**: Image `europe-west1-docker.pkg.dev/signalngn-prod/signalngn/trader:latest` is pulled by the tenant's Cloud Run service at deploy time. The repository is granted public (unauthenticated) read access by Signal ngn.

**Rationale**: Tenants don't have the source or Go toolchain. Build times would be ~5 min on Cloud Build per update. Pulling a published image is instant and ensures version consistency.

**Alternative considered**: Give tenants Cloud Build scripts to build from source — rejected due to complexity and maintenance surface.

---

### 3. Cloud SQL Postgres (db-f1-micro) per tenant

**Decision**: Each tenant gets their own Cloud SQL Postgres instance, smallest tier.

**Rationale**: The trader's SQLite/Postgres ledger is per-instance. Shared DB would couple tenants. `db-f1-micro` costs ~$7/month — acceptable for a dedicated trading node. The trader runs migrations on startup, so no separate migration step is needed.

**Alternative considered**: SQLite via a mounted Cloud Storage FUSE volume — rejected because Cloud Run's stateless nature makes persistent volume management complex and unreliable.

---

### 4. Static egress IP via Cloud NAT (Direct VPC Egress)

**Decision**: Reserve a static external IP, create a Cloud Router + Cloud NAT in the tenant's project, and deploy Cloud Run with `--network=default --subnet=default --vpc-egress=all-traffic`. This matches the pattern in `scripts/setup-static-ip.sh`.

**Rationale**: Brokers (Binance, etc.) allow IP-allowlisting of API keys as a security measure. Without a static IP, tenants cannot use this feature. Cloud NAT is the standard GCP mechanism for static egress from Cloud Run.

**Cost**: ~$1.50/month for the reserved static IP address.

---

### 5. Secrets in Secret Manager, not env vars

**Decision**: `DB_PASSWORD`, `NATS_CREDS`, and `SN_API_KEY` are stored as Secret Manager secrets and mounted via `--set-secrets` in the Cloud Run deployment. Non-sensitive config is passed as env vars.

**Rationale**: GCP best practice. Keeps sensitive material out of Cloud Run revision history and deployment logs. Matches the production pattern.

---

### 6. Service account with least-privilege IAM

**Decision**: Create a dedicated service account `trader-sa@<project>.iam.gserviceaccount.com` with only the roles required:
- `roles/cloudsql.client` — connect to Cloud SQL
- `roles/secretmanager.secretAccessor` — read secrets at runtime
- `roles/run.invoker` — not needed (service is public), but may be useful for health checks from other GCP services

No `roles/editor` or `roles/owner`.

---

### 7. Trading mode defaults to `paper`

**Decision**: Installer sets `TRADING_MODE=paper` and `TRADING_ENABLED=true`. To go live the tenant edits the Cloud Run env vars manually (via console or `deploy-tenant.sh`).

**Rationale**: Prevents accidental live trading on a misconfigured instance. The tenant should validate signals and ledger behaviour in paper mode first.

---

### 8. Script collects inputs upfront, then executes

**Decision**: The script prompts for all required inputs at the start (project ID, region, NATS creds file path, SN API key), validates them, then proceeds through non-interactive provisioning steps.

**Rationale**: Avoids the poor UX of prompts appearing mid-way through a long provisioning sequence. Tenant can walk away after answering the initial questions.

## Script Step Sequence (`install-tenant.sh`)

```
1.  Check prerequisites (gcloud installed, authenticated)
2.  Collect inputs:
      - GCP Project ID (validate exists + billing enabled)
      - Region (default: europe-west1)
      - Path to NATS .creds file (downloaded from spot-canvas-app)
      - SN API Key (paste from spot-canvas-app)
      - Trader account name (default: "main")
      - Portfolio size USD (default: 10000)
3.  Enable required GCP APIs:
      compute, run, sqladmin, secretmanager, artifactregistry
4.  Create service account (trader-sa) + bind IAM roles
5.  Reserve static external IP address (trader-nat-ip)
6.  Create Cloud Router + Cloud NAT
7.  Create Cloud SQL Postgres instance (db-f1-micro)
8.  Create database + user inside Cloud SQL
9.  Create Secret Manager secrets:
      - trader-db-password (random 32-char)
      - trader-nats-creds (content of .creds file)
      - trader-sn-api-key (from input)
10. Grant service account access to secrets
11. Deploy Cloud Run service (--allow-unauthenticated, VPC egress, secret mounts)
12. Wait for service health check to pass
13. Print summary:
      - Service URL
      - Static egress IP (with note to whitelist with broker)
      - Command to tail logs
      - How to enable live trading
```

## Risks / Trade-offs

- **Cloud SQL cold start** → Cloud SQL provisioning takes 5–10 min. Script waits with a spinner. Mitigation: clear messaging so tenant doesn't think it hung.
- **Artifact Registry access** → If Signal ngn restricts the registry to authenticated pulls in future, tenants would need a pull secret or a separate image hosting strategy. Mitigation: document the image source; keep public access for the tenant image.
- **NATS creds file on tenant disk** → After the install the file is no longer needed locally (it's in Secret Manager), but it exists on the tenant's machine. Mitigation: script offers to `shred` the local file after upload, and documents this in the install guide.
- **Region constraints** → Cloud SQL and Cloud Run must be in the same region for direct VPC connectivity. Script enforces single-region selection and uses it for all resources.
- **Script idempotency gaps** → Some `gcloud` operations (e.g., `create database`) aren't idempotent by default. Script wraps each step in an existence check. Mitigation: test re-run scenarios explicitly.

## Migration Plan

No migration needed — this is entirely new tooling. Deployment steps:

1. Add scripts to repo under `scripts/`
2. Add `docs/tenant-install.md`
3. Signal ngn team ensures Artifact Registry repo has public read access for the trader image
4. Signal ngn team documents the NATS creds issuance flow in spot-canvas-app (out of scope for this change)

## Open Questions

- ~~Static IP needed?~~ Yes — confirmed.
- Should the script support `--non-interactive` / flag-driven mode for automated provisioning? (Not in scope for v1, but the input-collection step should be structured to make this easy to add.)
- What image tag should tenants pin to — `latest` or a specific release tag? For v1 use `latest`; a future change can add versioned release management.
