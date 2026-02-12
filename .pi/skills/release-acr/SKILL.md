---
name: release-acr
description: Create a new ACR version tag and trigger the release workflow. Analyzes commits since the last tag, proposes a semver version, creates an annotated tag, and pushes to trigger CI/CD.
---

# Release ACR

Create a new version tag and trigger the release workflow.

## Prerequisites

Before starting, ensure we're on the default branch and up to date:

```bash
git checkout main && git pull
```

If there are uncommitted changes, stop and ask the user what to do.

## Steps

1. **Find the latest version tag:**

   ```bash
   git describe --tags --abbrev=0
   ```

2. **List all commits since that tag:**

   ```bash
   git log <tag>..HEAD --oneline
   ```

   If there are no commits since the last tag, inform the user and stop.

3. **Propose a version number** following semver by analyzing the changes:
   - **MAJOR**: breaking changes
   - **MINOR**: new features, backward compatible
   - **PATCH**: bug fixes, minor improvements

4. **Present the proposed version and changelog summary to the user.** Ask for approval, offering:
   - The proposed version
   - Alternative bumps (major/minor/patch from current)
   - Custom input option

5. **Once approved, create an annotated tag** with the changelog summary:

   ```bash
   git tag -a <version> --cleanup=whitespace -m "<summary of changes>"
   ```

   **Important**: The `--cleanup=whitespace` flag is required to preserve markdown headers (lines starting with `#`) in the tag message. Without it, git strips them as comments.

6. **Push the tag** to trigger the release workflow:

   ```bash
   git push origin <version>
   ```

   This triggers `.github/workflows/release.yml` which builds binaries for Linux/macOS (amd64/arm64), creates GitHub releases, and updates the Homebrew tap. The tag annotation is used as the GitHub Release body.

That's it. No further steps needed.

## Important

- Always wait for user approval before creating the tag
- The tag message should summarize the changes since the previous version
- Use conventional commit style for the tag message (list features, fixes, etc.)
- Release history is stored in git tag annotations and displayed on GitHub Releases
