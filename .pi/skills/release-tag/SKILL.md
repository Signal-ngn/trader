---
description: Commit staged/pending changes, create a release tag, and push to trigger Cloud Build deployment. Use when the user wants to release, deploy, or push a new version.
---

# Release Tag

Commit staged/pending changes, create a release tag, and push to trigger Cloud Build deployment.

## Usage

The user may optionally provide:
- **message**: Commit message. If not provided, generate one from the changes.
- **tag**: Explicit release tag. If not provided, auto-increment from the latest tag.

## Steps

1. Find the latest release tag and auto-increment the patch version:
   ```bash
   git tag --list 'release-*' --sort=-v:refname | head -1
   ```
   - If latest is `release-1.0.3`, next is `release-1.0.4`
   - If no tags exist, start with `release-1.0.0`
   - If user explicitly provides a tag, use that instead

2. Check for uncommitted changes. If there are staged or modified files, commit them.
   - If no commit message provided, generate one from the diff.

3. Create the git tag.

4. Push the branch and tags to origin:
   ```bash
   git push origin main --tags
   ```

5. Confirm the push succeeded and remind the user that Cloud Build will be triggered.

## Notes

- Tags must follow the `release-x.x.x` pattern (e.g. `release-1.0.0`) to trigger the Cloud Build pipeline.
- The Cloud Build config is in `cloudbuild-prod.yaml` — it builds the trader Docker image and deploys to Cloud Run in `signalngn-prod`.
- The trigger is in region `europe-west1`.
