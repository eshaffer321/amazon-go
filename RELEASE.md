# Release Process

This repo is a Go module. Releases are source releases published by pushing SemVer tags.

## Versioning

Use Semantic Versioning:

- Patch, `vX.Y.Z`, for bug fixes and parser compatibility fixes.
- Minor, `vX.Y.0`, for new APIs, new CLI commands, new importers, or compatible behavior changes.
- Major, `vX.0.0`, for breaking API or storage behavior.

## Pre-Release Checklist

1. Start from an up-to-date `main`.
2. Make sure `CHANGELOG.md` has an entry for the version being released.
3. Run the full local quality gate:

   ```bash
   make check
   ```

4. Confirm no sensitive or generated local files are staged:

   ```bash
   git status --short
   git diff --cached --stat
   ```

5. Commit the changelog and any final fixes.

## Tagging

Create and push an annotated tag:

```bash
VERSION=v0.2.0
git tag -a "$VERSION" -m "$VERSION"
git push origin "$VERSION"
```

Pushing a `v*` tag runs the release workflow. The workflow verifies the module, runs the same quality gate as CI, and creates a GitHub Release using the matching `CHANGELOG.md` section.

## Changelog Format

Keep an `[Unreleased]` section at the top. For each release, add a section like:

```markdown
## [0.2.0] - 2026-06-26

### Added
- ...

### Fixed
- ...
```

The release workflow expects a heading that matches the pushed tag without the leading `v`, for example tag `v0.2.0` maps to `## [0.2.0]`.

## After Release

1. Verify the GitHub Release was created.
2. Verify the module resolves:

   ```bash
   go list -m github.com/eshaffer321/amazon-go@v0.2.0
   ```

3. Add a fresh empty `[Unreleased]` section for future changes if needed.
