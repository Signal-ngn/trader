## Why

Signal ngn tenants need to run their own trader instance in their own Google Cloud account so that their trading engine is isolated, self-owned, and billed to their own GCP project. Today there is no guided path for a tenant to go from zero to a running Cloud Run trader — they would need to reverse-engineer the internal `cloudbuild-prod.yaml` and figure out secrets, IAM, and database setup on their own.

## What Changes

- New shell script `scripts/install-tenant.sh` that walks a tenant through the full GCP setup interactively
- New supporting script `scripts/deploy-tenant.sh` for subsequent redeployments (upgrade / config change)
- New documentation file `docs/tenant-install.md` explaining prerequisites, what the script does, and how to operate the service post-install
- The installer pulls the pre-built trader Docker image from the Signal ngn Artifact Registry (public read) rather than building from source, keeping the tenant setup simple

## Capabilities

### New Capabilities
- `tenant-installer`: Interactive shell script that provisions all GCP infrastructure (project assumed to exist with billing enabled) and deploys the trader to Cloud Run. Covers: enabling GCP APIs, creating a service account with least-privilege IAM roles, provisioning a Cloud SQL Postgres instance, creating Secret Manager secrets (DB password, NATS creds from Signal ngn, SN API key entered by the tenant), deploying the Cloud Run service with the correct env vars and secret mounts, and printing a summary with the service URL.

### Modified Capabilities
<!-- none -->

## Impact

- **New files**: `scripts/install-tenant.sh`, `scripts/deploy-tenant.sh`, `docs/tenant-install.md`
- **No existing code changes** — installer is purely operational tooling
- **Dependencies**: tenant must have `gcloud` CLI installed and authenticated, and a GCP project with billing enabled
- **Secrets sourced from two places**:
  - NATS credentials (`.creds` file) — issued by Signal ngn platform, tenant downloads from the spot-canvas-app web app
  - SN API key — created by the tenant in spot-canvas-app and entered interactively during install; stored in Secret Manager
  - DB password — generated randomly by the installer and stored in Secret Manager
- **Image source**: `europe-west1-docker.pkg.dev/signalngn-prod/signalngn/trader:latest` (tenants pull, do not build)
- **Database**: Cloud SQL Postgres (cheapest shared-core tier, `db-f1-micro`) created per tenant; schema applied via the trader's built-in migration on first boot
- **NATS**: tenants connect to the shared Signal ngn NATS cluster (`tls://connect.ngs.global`) using their issued `.creds` file — no tenant-side NATS infra needed
