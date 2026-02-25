#!/usr/bin/env bash
#
# Build, push, and deploy the ledger service to Cloud Run.
#
# Usage:
#   ./scripts/deploy.sh                          # Build + push + deploy to staging
#   ./scripts/deploy.sh --tag v1.2.3             # Custom tag
#   ./scripts/deploy.sh --push-only              # Build + push only, skip deploy
#   ./scripts/deploy.sh --dry-run                # Preview commands
#
# Options:
#   --project PROJECT   GCP project (default: spotcanvas-staging)
#   --region REGION     GCP region (default: europe-west3)
#   --env ENV           staging or production (default: staging)
#   --tag TAG           Image tag (default: staging)
#   --push-only         Build and push only, skip Cloud Run deploy
#   --dry-run           Print commands without executing
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Defaults
PROJECT="${GCP_PROJECT:-spotcanvas-staging}"
REGION="europe-west3"
ENV="staging"
TAG="staging"
REPOSITORY=""
IMAGE_NAME="ledger"
DRY_RUN=false
PUSH_ONLY=false

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }
log_step()  { echo -e "${BLUE}[STEP]${NC} $1"; }

usage() {
    sed -n '2,/^$/s/^# \?//p' "$0"
    exit 0
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --project)   PROJECT="$2"; shift 2 ;;
        --region)    REGION="$2"; shift 2 ;;
        --env)       ENV="$2"; shift 2 ;;
        --tag)       TAG="$2"; shift 2 ;;
        --push-only) PUSH_ONLY=true; shift ;;
        --dry-run)   DRY_RUN=true; shift ;;
        -h|--help)   usage ;;
        *) log_error "Unknown option: $1"; exit 1 ;;
    esac
done

run_cmd() {
    if [[ "$DRY_RUN" == true ]]; then
        echo "  \$ $*"
    else
        "$@"
    fi
}

# Validate
if [[ -z "$PROJECT" ]]; then
    log_error "GCP project not set. Use --project or set GCP_PROJECT"
    exit 1
fi

# Env-specific overrides (must come before derived values)
if [[ "$ENV" == "staging" ]]; then
    SERVICE_NAME="spot-canvas-ledger-staging"
    SECRET_PREFIX="spot-canvas-staging"
    REPOSITORY="${REPOSITORY:-ledger}"
else
    SERVICE_NAME="spot-canvas-ledger"
    SECRET_PREFIX="spot-canvas"
    REGION="europe-west1"
    REPOSITORY="${REPOSITORY:-signalngn}"
fi

# Derived values
ARTIFACT_REGISTRY="${REGION}-docker.pkg.dev/${PROJECT}/${REPOSITORY}"
IMAGE_URL="${ARTIFACT_REGISTRY}/${IMAGE_NAME}"
GIT_COMMIT=$(git -C "$PROJECT_ROOT" rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
CLOUDSQL_INSTANCE="${PROJECT}:${REGION}:spot-canvas-db"

echo ""
log_info "======================================"
log_info "Ledger — Build & Deploy"
log_info "======================================"
echo ""

if [[ "$DRY_RUN" == true ]]; then
    log_warn "DRY RUN — commands will be printed, not executed"
    echo ""
fi

log_info "Project:    $PROJECT"
log_info "Region:     $REGION"
log_info "Env:        $ENV"
log_info "Service:    $SERVICE_NAME"
log_info "Image:      ${IMAGE_URL}:${TAG}"
log_info "Commit:     $GIT_COMMIT"
echo ""

# ── Step 1: Configure Docker auth ────────────────────────────
log_step "Configuring Docker authentication..."
run_cmd gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet

# ── Step 2: Build image ──────────────────────────────────────
log_step "Building Docker image..."
run_cmd docker build \
    --platform linux/amd64 \
    --label "org.opencontainers.image.revision=${GIT_COMMIT}" \
    --label "org.opencontainers.image.created=${BUILD_TIME}" \
    --label "org.opencontainers.image.version=${TAG}" \
    -t "${IMAGE_URL}:${TAG}" \
    -t "${IMAGE_URL}:${GIT_COMMIT}" \
    "$PROJECT_ROOT"

if [[ "$TAG" != "latest" ]]; then
    run_cmd docker tag "${IMAGE_URL}:${TAG}" "${IMAGE_URL}:latest"
fi

# ── Step 3: Push image ───────────────────────────────────────
log_step "Pushing image to Artifact Registry..."
run_cmd docker push "${IMAGE_URL}:${TAG}"
run_cmd docker push "${IMAGE_URL}:${GIT_COMMIT}"
if [[ "$TAG" != "latest" ]]; then
    run_cmd docker push "${IMAGE_URL}:latest"
fi

log_info "Image pushed: ${IMAGE_URL}:${TAG}"

if [[ "$PUSH_ONLY" == true ]]; then
    echo ""
    log_info "Push-only mode — skipping Cloud Run deploy."
    exit 0
fi

# ── Step 4: Deploy to Cloud Run ──────────────────────────────
log_step "Deploying to Cloud Run..."

DEPLOY_CMD=(
    gcloud run deploy "$SERVICE_NAME"
    --project="$PROJECT"
    --region="$REGION"
    --image="${IMAGE_URL}:${TAG}"
    --platform=managed
    --memory=256Mi
    --cpu=1
    --min-instances=0
    --max-instances=3
    --concurrency=80
    --timeout=300s
    --port=8080
    --allow-unauthenticated
    --add-cloudsql-instances="$CLOUDSQL_INSTANCE"
    --set-env-vars="ENVIRONMENT=${ENV},LOG_LEVEL=info,CLOUDSQL_INSTANCE=${CLOUDSQL_INSTANCE},DB_NAME=spot_canvas,DB_USER=spot"
    --set-secrets="DB_PASSWORD=${SECRET_PREFIX}-db-password:latest,NATS_URLS=${SECRET_PREFIX}-nats-url:latest,NATS_CREDS=${SECRET_PREFIX}-nats-creds:latest"
)

run_cmd "${DEPLOY_CMD[@]}"

# ── Done ─────────────────────────────────────────────────────
echo ""
if [[ "$DRY_RUN" != true ]]; then
    URL=$(gcloud run services describe "$SERVICE_NAME" \
        --project="$PROJECT" \
        --region="$REGION" \
        --format="value(status.url)" 2>/dev/null || echo "unavailable")

    log_info "=============================================="
    log_info "DEPLOYMENT SUCCESSFUL"
    log_info "=============================================="
    echo ""
    echo "  Service URL:  $URL"
    echo "  Health check: ${URL}/health"
else
    log_info "Dry run complete."
fi

echo ""
echo "  Useful commands:"
echo "    gcloud run services logs read $SERVICE_NAME --project=$PROJECT --region=$REGION"
echo "    gcloud run services describe $SERVICE_NAME --project=$PROJECT --region=$REGION"
echo ""
