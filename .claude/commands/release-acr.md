# Release ACR

Create a new version tag and trigger the release workflow.

## Steps

1. Find the latest version tag:

   ```
   git describe --tags --abbrev=0
   ```

2. List all commits since that tag:

   ```
   git log <tag>..HEAD --oneline
   ```

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

7. Regenerate CHANGELOG.md from all tags and commit it:

   ```bash
   # Generate changelog from tag annotations
   {
     echo "# Changelog"
     echo ""
     echo "All notable changes to ACR are documented in this file."
     echo ""
     echo "This changelog is generated from git tag annotations."
     echo ""
     git for-each-ref --sort=-v:refname --format='## [%(refname:short)] - %(creatordate:short)

%(contents)' refs/tags
   } > CHANGELOG.md
   ```

   Review the generated CHANGELOG.md for formatting, then commit and push:
   ```
   git add CHANGELOG.md
   git commit -m "docs: update CHANGELOG.md for <version>"
   git push origin main
   ```

## Important

- Always wait for user approval before creating the tag
- The tag message should summarize the changes since the previous version
- Use conventional commit style for the tag message (list features, fixes, etc.)
- The CHANGELOG.md is regenerated from tags after each release - it reflects released versions only
