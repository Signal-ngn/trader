#!/usr/bin/env bash
#
# install-tenant.sh — Provision GCP infrastructure and deploy the Signal ngn trader
# to Cloud Run in your own GCP project.
#
# What this script does:
#   1.  Checks prerequisites (gcloud installed and authenticated)
#   2.  Collects all inputs upfront (project, region, account name, NATS creds, API key)
#   3.  Enables required GCP APIs
#   4.  Creates a least-privilege service account
#   5.  Reserves a static external IP + wires Cloud Router / Cloud NAT
#   6.  Stores secrets in Secret Manager (NATS creds, SN API key)
#   7.  Deploys the trader to Cloud Run (paper mode by default)
#   8.  Prints a summary with the service URL and your static egress IP
#
# Usage:
#   ./scripts/install-tenant.sh
#
# Prerequisites:
#   - gcloud CLI installed and authenticated  (https://cloud.google.com/sdk)
#   - A GCP project with billing enabled
#   - NATS .creds file downloaded from spot-canvas-app
#   - Signal ngn API key created in spot-canvas-app
#
# Re-running this script is safe — each step checks whether the resource
# already exists and skips it if so.
#

set -euo pipefail

# ── Constants ─────────────────────────────────────────────────
readonly TRADER_IMAGE="europe-west1-docker.pkg.dev/signalngn-prod/signalngn/trader:latest"
readonly SERVICE_NAME="trader"
readonly SA_NAME="trader-sa"
readonly ADDRESS_NAME="trader-nat-ip"
readonly ROUTER_NAME="trader-router"
readonly NAT_NAME="trader-nat"
readonly SECRET_NATS="trader-nats-creds"
readonly SECRET_API_KEY="trader-sn-api-key"

# ── Colors ────────────────────────────────────────────────────
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info()    { echo -e "${GREEN}[INFO]${NC}  $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC}  $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1" >&2; }
log_step()    { echo -e "${BLUE}[STEP]${NC}  $1"; }
log_value()   { echo -e "        ${CYAN}$1${NC}"; }
log_section() { echo ""; echo -e "${BOLD}$1${NC}"; echo ""; }

exists_resource() {
    "$@" &>/dev/null
}

# ── Banner ────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║   Signal ngn Trader — Tenant Installer       ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
echo ""

# ────────────────────────────────────────────────────────────────────────────
# STEP 1 — PREREQUISITES
# ────────────────────────────────────────────────────────────────────────────
log_section "Checking prerequisites..."

if ! command -v gcloud &>/dev/null; then
    log_error "gcloud CLI is not installed."
    echo "  Install it from: https://cloud.google.com/sdk/docs/install"
    exit 1
fi
log_info "gcloud CLI found: $(gcloud version --format='value(Google Cloud SDK)' 2>/dev/null | head -1)"

if ! gcloud auth print-access-token &>/dev/null; then
    log_error "gcloud is not authenticated."
    echo "  Run: gcloud auth login"
    exit 1
fi
log_info "gcloud authenticated as: $(gcloud config get-value account 2>/dev/null)"

# ────────────────────────────────────────────────────────────────────────────
# STEP 2 — COLLECT INPUTS
# ────────────────────────────────────────────────────────────────────────────
log_section "Collecting configuration..."

# GCP Project ID
DEFAULT_PROJECT="$(gcloud config get-value project 2>/dev/null || true)"
if [[ -n "$DEFAULT_PROJECT" ]]; then
    read -r -p "  GCP Project ID [${DEFAULT_PROJECT}]: " INPUT_PROJECT
    PROJECT="${INPUT_PROJECT:-$DEFAULT_PROJECT}"
else
    read -r -p "  GCP Project ID: " PROJECT
fi

if [[ -z "$PROJECT" ]]; then
    log_error "GCP Project ID is required."
    exit 1
fi

# Validate project exists
if ! gcloud projects describe "$PROJECT" &>/dev/null; then
    log_error "Project '$PROJECT' not found or you don't have access to it."
    exit 1
fi
log_info "Project: $PROJECT"

# Region
read -r -p "  Region [europe-west1]: " INPUT_REGION
REGION="${INPUT_REGION:-europe-west1}"
log_info "Region: $REGION"

# Trader account name
read -r -p "  Trader account name [main]: " INPUT_ACCOUNT
ACCOUNT_NAME="${INPUT_ACCOUNT:-main}"
log_info "Account name: $ACCOUNT_NAME"

# Portfolio size
read -r -p "  Portfolio size USD [10000]: " INPUT_PORTFOLIO
PORTFOLIO_SIZE="${INPUT_PORTFOLIO:-10000}"
log_info "Portfolio size: \$${PORTFOLIO_SIZE}"

# NATS .creds file
echo ""
echo "  You can download your NATS .creds file from spot-canvas-app → Settings → API Keys."
while true; do
    read -r -p "  Path to NATS .creds file: " NATS_CREDS_PATH
    NATS_CREDS_PATH="${NATS_CREDS_PATH/#\~/$HOME}"  # expand ~
    if [[ -z "$NATS_CREDS_PATH" ]]; then
        log_warn "Path is required."
    elif [[ ! -f "$NATS_CREDS_PATH" ]]; then
        log_warn "File not found: $NATS_CREDS_PATH"
    elif [[ ! -s "$NATS_CREDS_PATH" ]]; then
        log_warn "File is empty: $NATS_CREDS_PATH"
    else
        log_info "NATS creds file: $NATS_CREDS_PATH"
        break
    fi
done

# SN API key (no-echo)
echo ""
echo "  Create your Signal ngn API key at spot-canvas-app → Settings → API Keys."
while true; do
    read -r -s -p "  Signal ngn API key (input hidden): " SN_API_KEY
    echo ""
    if [[ -z "$SN_API_KEY" ]]; then
        log_warn "API key is required."
    else
        log_info "Signal ngn API key: received (${#SN_API_KEY} chars)"
        break
    fi
done

# Confirm before proceeding
SA_EMAIL="${SA_NAME}@${PROJECT}.iam.gserviceaccount.com"
echo ""
echo -e "${BOLD}  ── Summary of what will be created ──${NC}"
log_value "Project:        $PROJECT"
log_value "Region:         $REGION"
log_value "Service:        $SERVICE_NAME (Cloud Run)"
log_value "Service Acct:   $SA_EMAIL"
log_value "Static IP:      $ADDRESS_NAME"
log_value "Secrets:        $SECRET_NATS, $SECRET_API_KEY"
log_value "Image:          $TRADER_IMAGE"
log_value "Trading mode:   paper (safe default)"
echo ""
read -r -p "  Proceed? [y/N]: " CONFIRM
if [[ "${CONFIRM,,}" != "y" ]]; then
    log_warn "Aborted."
    exit 0
fi

# ────────────────────────────────────────────────────────────────────────────
# STEP 3 — ENABLE GCP APIS
# ────────────────────────────────────────────────────────────────────────────
log_section "3/8  Enabling required GCP APIs..."

APIS=(
    "compute.googleapis.com"
    "run.googleapis.com"
    "secretmanager.googleapis.com"
    "artifactregistry.googleapis.com"
    "iam.googleapis.com"
)

gcloud services enable "${APIS[@]}" --project="$PROJECT"
log_info "APIs enabled."

# ────────────────────────────────────────────────────────────────────────────
# STEP 4 — SERVICE ACCOUNT + IAM
# ────────────────────────────────────────────────────────────────────────────
log_section "4/8  Creating service account..."

if exists_resource gcloud iam service-accounts describe "$SA_EMAIL" --project="$PROJECT"; then
    log_warn "Service account '$SA_EMAIL' already exists — skipping creation."
else
    gcloud iam service-accounts create "$SA_NAME" \
        --display-name="Signal ngn Trader" \
        --project="$PROJECT"
    log_info "Service account created: $SA_EMAIL"
fi

log_step "Binding IAM role roles/secretmanager.secretAccessor..."
gcloud projects add-iam-policy-binding "$PROJECT" \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="roles/secretmanager.secretAccessor" \
    --condition=None \
    --quiet

log_info "IAM roles bound."

# ────────────────────────────────────────────────────────────────────────────
# STEP 5 — STATIC IP + CLOUD ROUTER + CLOUD NAT
# ────────────────────────────────────────────────────────────────────────────
log_section "5/8  Setting up static egress IP..."

log_step "Reserving static external IP ($ADDRESS_NAME)..."
if exists_resource gcloud compute addresses describe "$ADDRESS_NAME" \
        --region="$REGION" --project="$PROJECT"; then
    log_warn "Address '$ADDRESS_NAME' already exists — skipping."
else
    gcloud compute addresses create "$ADDRESS_NAME" \
        --region="$REGION" \
        --project="$PROJECT"
    log_info "Static IP reserved."
fi

log_step "Creating Cloud Router ($ROUTER_NAME)..."
if exists_resource gcloud compute routers describe "$ROUTER_NAME" \
        --region="$REGION" --project="$PROJECT"; then
    log_warn "Router '$ROUTER_NAME' already exists — skipping."
else
    gcloud compute routers create "$ROUTER_NAME" \
        --region="$REGION" \
        --network=default \
        --project="$PROJECT"
    log_info "Cloud Router created."
fi

log_step "Creating Cloud NAT ($NAT_NAME)..."
if exists_resource gcloud compute routers nats describe "$NAT_NAME" \
        --router="$ROUTER_NAME" --region="$REGION" --project="$PROJECT"; then
    log_warn "NAT '$NAT_NAME' already exists — skipping."
else
    gcloud compute routers nats create "$NAT_NAME" \
        --router="$ROUTER_NAME" \
        --region="$REGION" \
        --nat-external-ip-pool="$ADDRESS_NAME" \
        --nat-all-subnet-ip-ranges \
        --project="$PROJECT"
    log_info "Cloud NAT created."
fi

# ────────────────────────────────────────────────────────────────────────────
# STEP 6 — SECRET MANAGER
# ────────────────────────────────────────────────────────────────────────────
log_section "6/8  Storing secrets in Secret Manager..."

# NATS creds
log_step "Creating secret '$SECRET_NATS'..."
if exists_resource gcloud secrets describe "$SECRET_NATS" --project="$PROJECT"; then
    log_warn "Secret '$SECRET_NATS' already exists — adding new version."
else
    gcloud secrets create "$SECRET_NATS" \
        --replication-policy=automatic \
        --project="$PROJECT"
fi
gcloud secrets versions add "$SECRET_NATS" \
    --data-file="$NATS_CREDS_PATH" \
    --project="$PROJECT"
log_info "NATS creds stored in Secret Manager."

# SN API key
log_step "Creating secret '$SECRET_API_KEY'..."
if exists_resource gcloud secrets describe "$SECRET_API_KEY" --project="$PROJECT"; then
    log_warn "Secret '$SECRET_API_KEY' already exists — adding new version."
else
    gcloud secrets create "$SECRET_API_KEY" \
        --replication-policy=automatic \
        --project="$PROJECT"
fi
echo -n "$SN_API_KEY" | gcloud secrets versions add "$SECRET_API_KEY" \
    --data-file=- \
    --project="$PROJECT"
log_info "SN API key stored in Secret Manager."

# Offer to shred the local .creds file
echo ""
read -r -p "  Securely delete the local .creds file now that it's in Secret Manager? [y/N]: " SHRED_CONFIRM
if [[ "${SHRED_CONFIRM,,}" == "y" ]]; then
    if command -v shred &>/dev/null; then
        shred -u "$NATS_CREDS_PATH"
        log_info "File securely deleted: $NATS_CREDS_PATH"
    else
        rm -f "$NATS_CREDS_PATH"
        log_info "File deleted (shred not available): $NATS_CREDS_PATH"
    fi
else
    log_warn "Local .creds file kept at: $NATS_CREDS_PATH"
    log_warn "Consider deleting it manually — it is now stored in Secret Manager."
fi

# ────────────────────────────────────────────────────────────────────────────
# STEP 7 — DEPLOY CLOUD RUN
# ────────────────────────────────────────────────────────────────────────────
log_section "7/8  Deploying trader to Cloud Run..."

gcloud run deploy "$SERVICE_NAME" \
    --project="$PROJECT" \
    --region="$REGION" \
    --image="$TRADER_IMAGE" \
    --platform=managed \
    --service-account="$SA_EMAIL" \
    --allow-unauthenticated \
    --memory=512Mi \
    --cpu=1 \
    --min-instances=0 \
    --max-instances=1 \
    --set-env-vars="TRADING_MODE=paper,TRADING_ENABLED=true,ACCOUNT_NAME=${ACCOUNT_NAME},PORTFOLIO_SIZE_USD=${PORTFOLIO_SIZE},NATS_URL=tls://connect.ngs.global" \
    --set-secrets="NATS_CREDS=${SECRET_NATS}:latest,SN_API_KEY=${SECRET_API_KEY}:latest" \
    --network=default \
    --subnet=default \
    --vpc-egress=all-traffic

log_info "Cloud Run service deployed."

# ────────────────────────────────────────────────────────────────────────────
# STEP 8 — WAIT FOR HEALTHY + PRINT SUMMARY
# ────────────────────────────────────────────────────────────────────────────
log_section "8/8  Waiting for service to be ready..."

ATTEMPTS=0
MAX_ATTEMPTS=30
until gcloud run services describe "$SERVICE_NAME" \
        --project="$PROJECT" \
        --region="$REGION" \
        --format="value(status.conditions[0].status)" 2>/dev/null | grep -q "True"; do
    ATTEMPTS=$(( ATTEMPTS + 1 ))
    if [[ $ATTEMPTS -ge $MAX_ATTEMPTS ]]; then
        log_warn "Service did not report Ready within expected time."
        log_warn "Check logs: gcloud run services logs read $SERVICE_NAME --project=$PROJECT --region=$REGION"
        break
    fi
    echo -n "."
    sleep 5
done
echo ""

SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" \
    --project="$PROJECT" \
    --region="$REGION" \
    --format="value(status.url)" 2>/dev/null || echo "unavailable")

STATIC_IP=$(gcloud compute addresses describe "$ADDRESS_NAME" \
    --region="$REGION" \
    --project="$PROJECT" \
    --format="value(address)" 2>/dev/null || echo "unavailable")

echo ""
echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║   ✓  INSTALLATION COMPLETE                   ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${BOLD}Service URL:${NC}       ${CYAN}${SERVICE_URL}${NC}"
echo -e "  ${BOLD}Static egress IP:${NC}  ${CYAN}${STATIC_IP}${NC}"
echo -e "  ${BOLD}GCP Project:${NC}       ${PROJECT}"
echo -e "  ${BOLD}Trading mode:${NC}      paper  (safe default)"
echo ""
echo -e "  ${YELLOW}⚠  Broker IP allowlist:${NC}"
echo "     Add ${STATIC_IP} to your Binance (or other broker) API key's"
echo "     IP restriction list before enabling live trading."
echo ""
echo -e "  ${BOLD}Useful commands:${NC}"
echo "     # View logs"
echo "     gcloud run services logs read $SERVICE_NAME \\"
echo "       --project=$PROJECT --region=$REGION --limit=50"
echo ""
echo "     # Redeploy (upgrade image or change config)"
echo "     ./scripts/deploy-tenant.sh"
echo ""
echo "     # Enable live trading"
echo "     gcloud run services update $SERVICE_NAME \\"
echo "       --project=$PROJECT --region=$REGION \\"
echo "       --update-env-vars=TRADING_MODE=live"
echo ""
