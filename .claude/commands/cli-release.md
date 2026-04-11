---
name: CLI Release
description: Create a new vX.Y.Z tag and push it to trigger a GitHub Actions CLI release via GoReleaser
category: Deployment
tags: [release, cli, github]
---

Create a new CLI release by tagging main and pushing to trigger the GitHub Actions GoReleaser workflow.

## Steps

1. Run `git fetch --tags origin` to ensure all tags are up to date.
2. Find the latest `vX.Y.Z` tag by running:
   ```
   git tag --list 'v*' --sort=-version:refname | head -1
   ```
3. Determine the next version:
   - If the user provided an argument (e.g. `/cli-release 1.0.0`), use that as the version verbatim (add `v` prefix if not provided).
   - Otherwise, auto-increment the **patch** number from the latest tag.
4. Show the user:
   - The new tag name (`vX.Y.Z`)
   - The commits that will be included (run `git log --oneline <latest-v-tag>..HEAD`)
   - Ask the user to confirm before proceeding.
5. After confirmation:
   - Ensure you are on the `main` branch and it is clean (`git status --porcelain` is empty).
   - Create the tag: `git tag vX.Y.Z`
   - Push the tag: `git push origin vX.Y.Z`
6. Report success and tell the user to monitor the release at: https://github.com/Signal-ngn/trader/actions

## Important

- NEVER create or push the tag without explicit user confirmation.
- The tag must be on the `main` branch.
- The tag pattern must be `vX.Y.Z` (with `v` prefix, no other prefixes).
