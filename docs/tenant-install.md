# Tenant Installation Guide — Signal ngn Trader on Cloud Run

This guide walks you through deploying your own Signal ngn trader instance to Google Cloud Run in your own GCP project. The trader runs in **paper mode by default** — no real orders are placed until you explicitly enable live trading.

---

## Prerequisites

### Tools

| Tool | Required Version | Install |
|------|-----------------|---------|
| `gcloud` CLI | Any recent version | [cloud.google.com/sdk](https://cloud.google.com/sdk/docs/install) |

No other tools are needed. The installer pulls a pre-built Docker image — you do not need Docker, Go, or any other build toolchain.

### GCP Account

- A Google Cloud project with **billing enabled**
- Your `gcloud` account must have **Owner** (or equivalent) permissions on the project
- Run `gcloud auth login` and `gcloud config set project YOUR_PROJECT_ID` before starting

### Signal ngn Platform Inputs

You need two things from [spot-canvas-app](https://app.spotcanvas.io) before running the installer:

1. **NATS `.creds` file** — Download from _Settings → API Keys → Download NATS Credentials_. This file authenticates your trader to the Signal ngn signal stream.
2. **Signal ngn API key** — Create one at _Settings → API Keys → New API Key_. This is used by the trader to record trades and query your portfolio on the platform.

Keep both of these secure. The installer will upload them to GCP Secret Manager and (optionally) delete the local `.creds` file afterwards.

---

## What the Installer Does

Run the installer with:

```bash
./scripts/install-tenant.sh
```

The script collects all inputs upfront, then provisions everything without further interaction:

### Step 1 — Prerequisite checks
Verifies that `gcloud` is installed and you are authenticated.

### Step 2 — Input collection
Prompts for:
- GCP Project ID
- Region (default: `europe-west1`)
- Trader account name (default: `main`)
- Portfolio size in USD (default: `10000`)
- Path to your NATS `.creds` file
- Your Signal ngn API key (input is hidden)

Shows a summary and asks for confirmation before making any changes.

### Step 3 — Enable GCP APIs
Enables the following APIs on your project (idempotent — safe to run again):
- `compute.googleapis.com`
- `run.googleapis.com`
- `secretmanager.googleapis.com`
- `artifactregistry.googleapis.com`
- `iam.googleapis.com`

### Step 4 — Service account + IAM
Creates a dedicated service account `trader-sa@<project>.iam.gserviceaccount.com` and grants it `roles/secretmanager.secretAccessor`. The trader runs as this account — it has no other permissions.

### Step 5 — Static egress IP + Cloud NAT
Reserves a static external IP address (`trader-nat-ip`) and wires it through a Cloud Router + Cloud NAT so all outbound traffic from your trader exits from a predictable IP address.

This is required if you want to use Binance's (or another broker's) IP allowlisting feature on your API key — see [Whitelist your egress IP](#whitelist-your-egress-ip-with-your-broker) below.

**Cost:** ~$1.50/month for the reserved static IP.

### Step 6 — Secrets in Secret Manager
Uploads your NATS credentials and Signal ngn API key to GCP Secret Manager. The secrets are named:
- `trader-nats-creds`
- `trader-sn-api-key`

The installer never writes these to disk or leaves them in environment variables after the script exits. It also offers to securely delete the local `.creds` file once uploaded.

### Step 7 — Deploy to Cloud Run
Deploys the trader using the pre-built image:

```
europe-west1-docker.pkg.dev/signalngn-prod/signalngn/trader:latest
```

The service is configured with:
- `TRADING_MODE=paper` — no real orders until you change this
- `TRADING_ENABLED=true`
- NATS and API key secrets mounted from Secret Manager
- VPC egress routed through Cloud NAT (for the static IP)

### Step 8 — Health check + summary
Waits for the Cloud Run service to report `Ready`, then prints your service URL, static egress IP, and useful commands.

---

## Post-Install Operations

### View logs

```bash
gcloud run services logs read trader \
  --project=YOUR_PROJECT \
  --region=YOUR_REGION \
  --limit=50
```

Or stream live:

```bash
gcloud run services logs tail trader \
  --project=YOUR_PROJECT \
  --region=YOUR_REGION
```

### Redeploy (upgrade image or change config)

Use the deploy script for subsequent updates. It pulls the latest image and re-applies secrets without touching Cloud NAT or Secret Manager:

```bash
./scripts/deploy-tenant.sh
```

To change a non-secret config value (e.g. portfolio size):

```bash
gcloud run services update trader \
  --project=YOUR_PROJECT \
  --region=YOUR_REGION \
  --update-env-vars=PORTFOLIO_SIZE_USD=25000
```

### Rotate secrets

To rotate your NATS credentials or API key, upload a new version to Secret Manager and then redeploy so Cloud Run picks up the latest:

```bash
# Rotate NATS creds
gcloud secrets versions add trader-nats-creds \
  --data-file=/path/to/new.creds \
  --project=YOUR_PROJECT

# Rotate SN API key
echo -n "NEW_API_KEY" | gcloud secrets versions add trader-sn-api-key \
  --data-file=- \
  --project=YOUR_PROJECT

# Redeploy to pick up the new secret versions
./scripts/deploy-tenant.sh
```

### Enable live trading

The installer deploys in `paper` mode (simulated orders only). Once you have validated that signals are flowing and positions are being recorded correctly, you can enable live trading:

```bash
gcloud run services update trader \
  --project=YOUR_PROJECT \
  --region=YOUR_REGION \
  --update-env-vars=TRADING_MODE=live
```

> ⚠️ **Before enabling live trading:**
> 1. Confirm paper mode is working — check the platform portfolio at spot-canvas-app
> 2. Whitelist your static egress IP with your broker (see below)
> 3. Verify your broker API key has the correct permissions (spot trading, read balances)

To switch back to paper mode at any time:

```bash
gcloud run services update trader \
  --project=YOUR_PROJECT \
  --region=YOUR_REGION \
  --update-env-vars=TRADING_MODE=paper
```

---

## Whitelist your egress IP with your broker

Your trader's outbound traffic exits through a static IP reserved during installation. To find it:

```bash
gcloud compute addresses describe trader-nat-ip \
  --region=YOUR_REGION \
  --project=YOUR_PROJECT \
  --format="value(address)"
```

Or it was printed at the end of the `install-tenant.sh` run.

### Binance

1. Log in to Binance and go to **API Management**
2. Edit your API key
3. Under **IP access restrictions**, select **Restrict access to trusted IPs only**
4. Add your static egress IP
5. Save

Without this, Binance may reject API calls from the trader depending on your API key's security settings. The IP restriction also protects your key — even if it were leaked, it can only be used from your trader's IP.

Other brokers have similar IP allowlisting features — consult their documentation.

---

## Re-running the installer

The installer is idempotent. If a step fails partway through, you can re-run `install-tenant.sh` with the same inputs. Each step checks whether the resource already exists and skips it if so. A new secret version will be added for the NATS creds and API key — old versions are retained in Secret Manager and can be cleaned up manually if desired.

---

## Uninstalling

To tear down everything the installer created:

```bash
# Delete the Cloud Run service
gcloud run services delete trader --project=YOUR_PROJECT --region=YOUR_REGION

# Delete secrets
gcloud secrets delete trader-nats-creds --project=YOUR_PROJECT
gcloud secrets delete trader-sn-api-key --project=YOUR_PROJECT

# Delete Cloud NAT, Router, static IP
gcloud compute routers nats delete trader-nat \
  --router=trader-router --region=YOUR_REGION --project=YOUR_PROJECT
gcloud compute routers delete trader-router \
  --region=YOUR_REGION --project=YOUR_PROJECT
gcloud compute addresses delete trader-nat-ip \
  --region=YOUR_REGION --project=YOUR_PROJECT

# Delete service account
gcloud iam service-accounts delete \
  trader-sa@YOUR_PROJECT.iam.gserviceaccount.com \
  --project=YOUR_PROJECT
```
