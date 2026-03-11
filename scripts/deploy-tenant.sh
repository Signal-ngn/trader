#!/usr/bin/env bash
#
# deploy-tenant.sh — Redeploy the Signal ngn trader on Cloud Run.
#
# Use this script to upgrade to the latest image or apply config changes
# without re-running the full installer. Secrets and VPC config are preserved.
#
# Usage:
#   ./scripts/deploy-tenant.sh
#
# Prerequisites:
#   - gcloud CLI installed and authenticated
#   - Trader already installed via install-tenant.sh
#

set -euo pipefail

# ── Constants (must match install-tenant.sh) ──────────────────
readonly TRADER_IMAGE="europe-west1-docker.pkg.dev/signalngn-prod/signalngn/trader:latest"
readonly SERVICE_NAME="trader"
readonly SA_NAME="trader-sa"
readonly SECRET_NATS="trader-nats-creds"
readonly SECRET_API_KEY="trader-sn-api-key"

# ── Colors ────────────────────────────────────────────────────
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log_info()    { echo -e "${GREEN}[INFO]${NC}  $1"; }
log_warn()    { echo -e "${YELLOW}[WARN]${NC}  $1"; }
log_step()    { echo -e "${BLUE}[STEP]${NC}  $1"; }
log_section() { echo ""; echo -e "${BOLD}$1${NC}"; echo ""; }

# ── Banner ────────────────────────────────────────────────────
echo ""
echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║   Signal ngn Trader — Redeploy               ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
echo ""

# ── Prerequisites ─────────────────────────────────────────────
log_section "Checking prerequisites..."

if ! command -v gcloud &>/dev/null; then
    echo -e "\033[0;31m[ERROR]\033[0m gcloud CLI is not installed."
    echo "  Install it from: https://cloud.google.com/sdk/docs/install"
    exit 1
fi

if ! gcloud auth print-access-token &>/dev/null; then
    echo -e "\033[0;31m[ERROR]\033[0m gcloud is not authenticated."
    echo "  Run: gcloud auth login"
    exit 1
fi
log_info "Authenticated as: $(gcloud config get-value account 2>/dev/null)"

# ── Collect inputs ────────────────────────────────────────────
log_section "Configuration..."

DEFAULT_PROJECT="$(gcloud config get-value project 2>/dev/null || true)"
if [[ -n "$DEFAULT_PROJECT" ]]; then
    read -r -p "  GCP Project ID [${DEFAULT_PROJECT}]: " INPUT_PROJECT
    PROJECT="${INPUT_PROJECT:-$DEFAULT_PROJECT}"
else
    read -r -p "  GCP Project ID: " PROJECT
fi

if [[ -z "$PROJECT" ]]; then
    echo -e "\033[0;31m[ERROR]\033[0m GCP Project ID is required."
    exit 1
fi

read -r -p "  Region [europe-west1]: " INPUT_REGION
REGION="${INPUT_REGION:-europe-west1}"

SA_EMAIL="${SA_NAME}@${PROJECT}.iam.gserviceaccount.com"

log_info "Project: $PROJECT"
log_info "Region:  $REGION"
log_info "Image:   $TRADER_IMAGE"
echo ""
read -r -p "  Proceed with redeployment? [y/N]: " CONFIRM
if [[ "${CONFIRM,,}" != "y" ]]; then
    log_warn "Aborted."
    exit 0
fi

# ── Redeploy ──────────────────────────────────────────────────
log_section "Redeploying Cloud Run service..."

log_step "Pulling latest image and updating service..."

gcloud run deploy "$SERVICE_NAME" \
    --project="$PROJECT" \
    --region="$REGION" \
    --image="$TRADER_IMAGE" \
    --platform=managed \
    --service-account="$SA_EMAIL" \
    --set-secrets="NATS_CREDS=${SECRET_NATS}:latest,SN_API_KEY=${SECRET_API_KEY}:latest"

log_info "Redeployment complete."

# ── Summary ───────────────────────────────────────────────────
echo ""
SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" \
    --project="$PROJECT" \
    --region="$REGION" \
    --format="value(status.url)" 2>/dev/null || echo "unavailable")

echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║   ✓  REDEPLOYMENT COMPLETE                   ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
echo ""
echo -e "  ${BOLD}Service URL:${NC}  ${CYAN}${SERVICE_URL}${NC}"
echo ""
echo "  Useful commands:"
echo "     gcloud run services logs read $SERVICE_NAME \\"
echo "       --project=$PROJECT --region=$REGION --limit=50"
echo ""
