## 1. Install Script — Prerequisites & Input Collection

- [x] 1.1 Create `scripts/install-tenant.sh` with executable bit and a `set -euo pipefail` header with usage banner
- [x] 1.2 Add prerequisite checks: verify `gcloud` is installed and authenticated (`gcloud auth print-access-token` succeeds)
- [x] 1.3 Collect inputs upfront: GCP project ID (validate project exists), region (default `europe-west1`), trader account name (default `main`), portfolio size USD (default `10000`)
- [x] 1.4 Prompt for path to NATS `.creds` file and validate the file exists and is non-empty
- [x] 1.5 Prompt for SN API key (no-echo input via `read -s`) and validate non-empty

## 2. Install Script — GCP Infrastructure Provisioning

- [x] 2.1 Enable required GCP APIs: `compute.googleapis.com`, `run.googleapis.com`, `secretmanager.googleapis.com`, `artifactregistry.googleapis.com`
- [x] 2.2 Create service account `trader-sa@<project>.iam.gserviceaccount.com` (skip if already exists)
- [x] 2.3 Bind IAM role `roles/secretmanager.secretAccessor` to the service account
- [x] 2.4 Reserve a static external IP address named `trader-nat-ip` in the chosen region (skip if already exists)
- [x] 2.5 Create a Cloud Router named `trader-router` in the region (skip if already exists)
- [x] 2.6 Create a Cloud NAT config on the router using the reserved static IP (skip if already exists)

## 3. Install Script — Secret Manager

- [x] 3.1 Create secret `trader-nats-creds` and upload the contents of the provided `.creds` file as the first version (skip creation if secret already exists; always add a new version)
- [x] 3.2 Create secret `trader-sn-api-key` and upload the entered API key as the first version (same idempotency pattern)
- [x] 3.3 Grant the `trader-sa` service account `roles/secretmanager.secretAccessor` on each secret (project-level binding already done in 2.3; this step confirms secret-level access if needed)
- [x] 3.4 Offer to securely delete the local `.creds` file after upload (prompt with `y/N`; use `shred -u` if available, else `rm`)

## 4. Install Script — Cloud Run Deployment

- [x] 4.1 Deploy the Cloud Run service using image `europe-west1-docker.pkg.dev/signalngn-prod/signalngn/trader:latest`
- [x] 4.2 Set non-sensitive env vars: `TRADING_MODE=paper`, `TRADING_ENABLED=true`, `ACCOUNT_NAME=<input>`, `PORTFOLIO_SIZE_USD=<input>`, `NATS_URL=tls://connect.ngs.global`
- [x] 4.3 Mount secrets via `--set-secrets`: `NATS_CREDS=trader-nats-creds:latest`, `SN_API_KEY=trader-sn-api-key:latest`
- [x] 4.4 Configure VPC egress: `--network=default --subnet=default --vpc-egress=all-traffic`
- [x] 4.5 Set `--service-account=trader-sa@<project>.iam.gserviceaccount.com` and `--allow-unauthenticated`
- [x] 4.6 Wait for the service to become healthy (poll `gcloud run services describe` until status is `Ready`)

## 5. Install Script — Completion Summary

- [x] 5.1 Print completion banner with: Cloud Run service URL, static egress IP address, command to tail logs (`gcloud run services logs read …`), and instruction to enable live trading (edit `TRADING_MODE` env var)
- [x] 5.2 Print note to whitelist the static egress IP with broker API key settings

## 6. Deploy Script

- [x] 6.1 Create `scripts/deploy-tenant.sh` with `set -euo pipefail`, prerequisite check (gcloud auth), and upfront prompts for project ID and region
- [x] 6.2 Re-deploy the Cloud Run service with `--image=…:latest` and `--update-env-vars` / `--set-secrets` matching the install script configuration (leave secrets and VPC config untouched)

## 7. Documentation

- [x] 7.1 Create `docs/tenant-install.md` with a Prerequisites section: required tools (`gcloud` CLI), GCP permissions (project owner or equivalent), and external inputs (NATS `.creds` file, SN API key) sourced from spot-canvas-app
- [x] 7.2 Add a Step-by-Step section explaining what `install-tenant.sh` does at each stage (mirrors the script sequence in the design)
- [x] 7.3 Add a Post-Install Operations section: how to view logs, how to redeploy with `deploy-tenant.sh`, how to rotate secrets in Secret Manager, and how to enable live trading
- [x] 7.4 Add a note about the static egress IP: what it is, where to find it, and how to whitelist it with broker (Binance) API key settings
