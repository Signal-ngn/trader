## ADDED Requirements

### Requirement: Install script provisions GCP infrastructure interactively
The installer (`scripts/install-tenant.sh`) SHALL guide a tenant through the full GCP setup in a single interactive session. It SHALL assume a GCP project exists with billing enabled, and SHALL require the tenant to be authenticated via `gcloud` CLI before running.

#### Scenario: Prerequisite check passes
- **WHEN** the tenant runs `install-tenant.sh` with `gcloud` authenticated and a valid project set
- **THEN** the script proceeds to infrastructure provisioning without errors

#### Scenario: Prerequisite check fails — gcloud not authenticated
- **WHEN** the tenant runs `install-tenant.sh` without being authenticated
- **THEN** the script exits with a clear error message instructing the tenant to run `gcloud auth login`

#### Scenario: Prerequisite check fails — no project set
- **WHEN** no active GCP project is configured in `gcloud`
- **THEN** the script exits with a clear error message instructing the tenant to run `gcloud config set project <PROJECT_ID>`

### Requirement: Install script enables required GCP APIs
The installer SHALL enable all required GCP service APIs before attempting to create any resources.

#### Scenario: APIs enabled successfully
- **WHEN** the installer runs in a project where the required APIs are not yet enabled
- **THEN** the script enables `run.googleapis.com`, `sqladmin.googleapis.com`, `secretmanager.googleapis.com`, `iam.googleapis.com`, and `artifactregistry.googleapis.com`

#### Scenario: APIs already enabled
- **WHEN** the APIs are already enabled in the project
- **THEN** the enable command is idempotent and the script continues without error

### Requirement: Install script creates a least-privilege service account
The installer SHALL create a dedicated GCP service account for the Cloud Run trader and grant it only the IAM roles required to operate.

#### Scenario: Service account created with required roles
- **WHEN** the installer creates the service account
- **THEN** it is granted `roles/cloudsql.client`, `roles/secretmanager.secretAccessor`, and `roles/run.invoker`

#### Scenario: Service account already exists
- **WHEN** the installer runs again and the service account already exists
- **THEN** the script does not fail; it reuses the existing account and ensures role bindings are applied

### Requirement: Install script provisions a Cloud SQL Postgres instance
The installer SHALL create a Cloud SQL Postgres instance using the cheapest shared-core tier, and create a dedicated database and user for the trader.

#### Scenario: Cloud SQL instance created
- **WHEN** the installer provisions the database
- **THEN** it creates a `db-f1-micro` tier Postgres instance in the same region as the Cloud Run service

#### Scenario: Database and user created
- **WHEN** the Cloud SQL instance is ready
- **THEN** the installer creates a dedicated database (e.g. `trader`) and a database user with a randomly generated password

### Requirement: Install script stores secrets in Secret Manager
The installer SHALL store all sensitive values in GCP Secret Manager and SHALL NOT write them to local files or environment variables that persist after the script exits.

#### Scenario: DB password stored in Secret Manager
- **WHEN** the installer generates the database password
- **THEN** it stores it as a new secret version in Secret Manager under a predictable name (e.g. `trader-db-password`)

#### Scenario: NATS credentials file stored in Secret Manager
- **WHEN** the installer prompts the tenant for the path to their `.creds` file issued by Signal ngn
- **THEN** the file contents are uploaded as a secret (e.g. `trader-nats-creds`) in Secret Manager

#### Scenario: SN API key stored in Secret Manager
- **WHEN** the installer prompts the tenant to enter their Signal ngn API key interactively
- **THEN** the key is stored as a secret (e.g. `trader-sn-api-key`) in Secret Manager and is not echoed to the terminal

### Requirement: Install script deploys the trader to Cloud Run
The installer SHALL deploy the trader using the pre-built image from the Signal ngn Artifact Registry without requiring the tenant to build any code.

#### Scenario: Cloud Run service deployed with correct image
- **WHEN** the installer deploys the service
- **THEN** it uses the image `europe-west1-docker.pkg.dev/signalngn-prod/signalngn/trader:latest`

#### Scenario: Cloud Run service configured with env vars and secret mounts
- **WHEN** the Cloud Run service is deployed
- **THEN** the DB connection string is derived from Cloud SQL, and NATS creds, DB password, and SN API key are mounted from Secret Manager as environment variables or volume mounts

#### Scenario: Cloud Run service uses the dedicated service account
- **WHEN** the Cloud Run service is deployed
- **THEN** it runs as the service account created by the installer

### Requirement: Install script prints a completion summary
After a successful install the script SHALL print a summary so the tenant knows their service is running and how to access it.

#### Scenario: Summary printed after successful install
- **WHEN** all resources are provisioned and the Cloud Run service is deployed
- **THEN** the script prints the Cloud Run service URL, the GCP project ID, the Cloud SQL instance name, and instructions for how to redeploy using `deploy-tenant.sh`

### Requirement: Deploy script supports subsequent redeployments
`scripts/deploy-tenant.sh` SHALL allow a tenant to redeploy the trader (image upgrade or config change) without re-running the full installer.

#### Scenario: Redeploy updates the Cloud Run service
- **WHEN** the tenant runs `deploy-tenant.sh` with `gcloud` authenticated and the correct project set
- **THEN** it re-deploys the Cloud Run service with the latest image and current secret versions, leaving Cloud SQL and Secret Manager secrets untouched

### Requirement: Tenant install documentation covers prerequisites and operations
`docs/tenant-install.md` SHALL document what the tenant needs before running the installer, what the installer does step-by-step, and how to operate the service after install.

#### Scenario: Documentation covers prerequisites
- **WHEN** a tenant reads `docs/tenant-install.md`
- **THEN** they can identify all required tools (`gcloud`), permissions (project owner or equivalent), and external inputs (NATS `.creds` file, SN API key) before starting

#### Scenario: Documentation covers post-install operations
- **WHEN** a tenant's service is running
- **THEN** the documentation explains how to view logs, redeploy with `deploy-tenant.sh`, and rotate secrets
