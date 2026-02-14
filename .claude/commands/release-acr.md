# Release ACR

Create a new version tag and trigger the release workflow.

## Prerequisites

Before starting, ensure we're on the default branch and up to date:

```
git checkout main && git pull
```

If there are uncommitted changes, stop and ask the user what to do.

## Steps

1. Find the latest version tag:

   ```
   git describe --tags --abbrev=0
   ```

2. List all commits since that tag:

   ```
   git log <tag>..HEAD --oneline
   ```

   If there are no commits since the last tag, inform the user and stop.

3. Analyze the changes and propose a version number following semver:
   - MAJOR: breaking changes
   - MINOR: new features, backward compatible
   - PATCH: bug fixes, minor improvements

4. Present the proposed version and changelog summary to the user. Ask for approval using AskUserQuestion with options for the proposed version, alternative versions (bump major/minor/patch from current), and custom input.

5. Once approved, create an annotated tag with the changelog summary:

   ```
   git tag -a <version> --cleanup=whitespace -m "<summary of changes>"
   ```

   **Important**: The `--cleanup=whitespace` flag is required to preserve markdown headers (lines starting with `#`) in the tag message. Without it, git strips them as comments.

6. Push the tag to trigger the release workflow:
   ```
   git push origin <version>
   ```

   This triggers `.github/workflows/release.yml` which builds binaries for Linux/macOS (amd64/arm64), creates GitHub releases, and updates the Homebrew tap.

## Important

- Always wait for user approval before creating the tag
- The tag message should summarize the changes since the previous version
- Use conventional commit style for the tag message (list features, fixes, etc.)
- Change history lives in GitHub release notes and annotated git tags (`git tag -l -n999`)
