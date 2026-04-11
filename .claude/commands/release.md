---
name: Release
description: Create a new release tag and push it to trigger a production Cloud Build deployment
category: Deployment
tags: [release, deploy, production]
---

Create a new production release by tagging main and pushing to trigger Cloud Build.

## Steps

1. Run `git fetch --tags origin` to ensure all tags are up to date.
2. Find the latest `release-X.Y.Z` tag by running:
   ```
   git tag --list 'release-*' --sort=-version:refname | head -1
   ```
3. Determine the next version:
   - If the user provided an argument (e.g. `/release 1.0.0`), use that as the version verbatim.
   - Otherwise, auto-increment the **patch** number from the latest tag. If no `release-*` tag exists, start at `0.1.0`.
4. Show the user:
   - The new tag name (`release-X.Y.Z`)
   - The commits that will be included (run `git log --oneline <latest-release-tag>..HEAD` or `git log --oneline -10` if no previous tag)
   - Ask the user to confirm before proceeding.
5. After confirmation:
   - Ensure you are on the `main` branch and it is clean (`git status --porcelain` is empty).
   - Create the tag: `git tag release-X.Y.Z`
   - Push the tag: `git push origin release-X.Y.Z`
6. Report success and remind the user to monitor the Cloud Build in the GCP console.

## Important

- NEVER create or push the tag without explicit user confirmation.
- The tag must be on the `main` branch.
- The tag pattern must match `release-X.Y.Z` exactly (no `v` prefix).
