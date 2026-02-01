# GitHub Workflows Overhaul - Task List

Comprehensive audit and modernization of CI/CD workflows for the Gearbox monorepo.

## Table of Contents

- [Audit Summary](#audit-summary)
- [Phase 1: Cleanup and Fixes](#phase-1-cleanup-and-fixes)
- [Phase 2: CI Workflow Improvements](#phase-2-ci-workflow-improvements)
- [Phase 3: Security Hardening](#phase-3-security-hardening)
- [Phase 4: Release Pipeline Improvements](#phase-4-release-pipeline-improvements)
- [Phase 5: Monthly Automation](#phase-5-monthly-automation)
- [Phase 6: Dependency Management](#phase-6-dependency-management)
- [Phase 7: New Workflows](#phase-7-new-workflows)
- [Phase 8: Deletions](#phase-8-deletions)
- [Reference: Current vs Target State](#reference-current-vs-target-state)

---

## Audit Summary

### Current Workflows

| Workflow | File | Status | Issues Found |
|----------|------|--------|-------------|
| CI (Dashboard) | `ci.yml` | Needs updates | Stale naming, no `go-version-file`, templ not pinned |
| CI (Agent) | `ci-agent.yml` | Needs updates | No `go-version-file`, missing gearbox-agent Docker ecosystem |
| Docker (Dashboard) | `docker.yml` | Good | Minor improvements possible |
| Docker (Agent) | `docker-agent.yml` | Good | Minor improvements possible |
| Release | `release.yml` | Needs updates | Stale `haproxy-monitor` naming, no cosign, no SBOM |
| Security | `security.yml` | Needs updates | `codeql-action@v3` (deprecated Dec 2026), gosec@master unpinned, missing Docker image scan |

### Key Findings

1. **Stale naming**: `haproxy-monitor` still used in `ci.yml`, `release.yml`, and Docker release job (rebrand to `gearbox` incomplete)
2. **Competing dependency managers**: Both Dependabot and Renovate configured (creates duplicate PRs)
3. **No monthly automation**: No scheduled builds for system package updates or automated releases
4. **No dependabot auto-merge workflow**: Missing workflow to auto-merge patch/minor dependency PRs
5. **Missing Docker image scanning**: Trivy only scans the repo filesystem, not the built Docker images
6. **No CodeQL analysis**: Not using GitHub's CodeQL for deeper static analysis
7. **Unpinned tool versions**: `templ@latest`, `gosec@master`, `golangci-lint: latest`
8. **No SBOM generation**: No software bill of materials for releases
9. **No cosign signing**: Release artifacts and Docker images are unsigned
10. **`codeql-action@v3`**: Used in `security.yml` but v3 is deprecated December 2026; should move to v4
11. **Missing agent Docker in dependabot**: Only `gearbox/` Dockerfile tracked, not `gearbox-agent/`
12. **No `go-version-file`**: All workflows hardcode `go-version: '1.25'` instead of reading from `go.mod`
13. **Missing PR checks**: No required status checks configuration documented
14. **No workflow to handle Dependabot PR bundling**: Dependabot groups help, but GitHub Actions + Go version bumps land in separate PRs

---

## Phase 1: Cleanup and Fixes

### 1.1 Fix stale `haproxy-monitor` naming in workflows

- [ ] `ci.yml`: Rename binary output from `haproxy-monitor` to `gearbox` (line 111, 117-118)
- [ ] `ci.yml`: Rename artifact name from `haproxy-monitor-${{ github.sha }}` to `gearbox-${{ github.sha }}`
- [ ] `release.yml` `build-monitor-binaries` job: Rename to `build-dashboard-binaries`
- [ ] `release.yml`: Rename binary from `haproxy-monitor-*` to `gearbox-*` in build step
- [ ] `release.yml` `build-docker` job: Change image name from `haproxy-monitor` to `gearbox` (lines 232, 257-258)
- [ ] `release.yml`: Verify `./cmd/server` is the correct entry point (may have been renamed)

### 1.2 Use `go-version-file` instead of hardcoded version

- [ ] All workflows: Replace `go-version: '1.25'` with `go-version-file: gearbox/go.mod` or `gearbox-agent/go.mod`
- [ ] Remove `cache-dependency-path` since `actions/setup-go@v6` infers cache from `go.mod` by default

This ensures Go version is managed in one place (`go.mod`) and Dependabot can bump it.

### 1.3 Pin tool versions

- [ ] Pin `templ` to a specific version instead of `@latest` (use version from `go.mod`: `v0.3.977`)
- [ ] Pin `golangci-lint-action` to a specific lint version instead of `version: latest`
- [ ] Pin `gosec` action to a tagged release instead of `@master`

---

## Phase 2: CI Workflow Improvements

### 2.1 Add Templ version check to CI

- [ ] Add a step that verifies generated `*_templ.go` files are up-to-date (run `templ generate` then `git diff --exit-code` on generated files)
- [ ] This catches cases where someone edited `.templ` files but forgot to regenerate

### 2.2 Add build verification for PRs

- [ ] `ci.yml`: Make build job depend on lint + test (currently all three run independently)
- [ ] Add `needs: [test, lint]` to the build job so it only runs if lint and test pass

### 2.3 Consolidate CI path filtering

- [ ] `ci.yml`: Add path filters for `gearbox/**` (currently runs on all pushes to main/develop, even agent-only changes)
- [ ] Both CI workflows: Add `.github/workflows/ci*.yml` to paths to re-trigger on workflow changes

### 2.4 Drop `develop` branch triggers (if not used)

- [ ] Verify if `develop` branch is actively used; if not, remove from CI triggers to reduce noise
- [ ] If used, keep but add path filters consistently

---

## Phase 3: Security Hardening

### 3.1 Upgrade `codeql-action` from v3 to v4

- [ ] `security.yml`: Update all `github/codeql-action/upload-sarif@v3` to `@v4`
- [ ] v3 is deprecated December 2026; v4 uses CodeQL bundle 2.24.0+

### 3.2 Add CodeQL analysis workflow

- [ ] Create new workflow `codeql.yml` for deep static analysis
- [ ] Configure for Go language
- [ ] Run on push to main, PRs, and weekly schedule
- [ ] Upload results to GitHub Security tab

### 3.3 Add Docker image scanning with Trivy

- [ ] Add Trivy container image scan to `docker.yml` after build (scan the built image, not just the repo)
- [ ] Add Trivy container image scan to `docker-agent.yml` after build
- [ ] Upload SARIF results to GitHub Security tab

### 3.4 Pin GitHub Actions to SHA hashes

- [ ] Pin all third-party actions to full commit SHAs (not mutable tags)
- [ ] Add a comment with the version tag next to each SHA for readability
- [ ] Example: `uses: actions/checkout@<sha> # v6`
- [ ] Use StepSecurity or manual pinning for all workflows

### 3.5 Add `step-security/harden-runner` to workflows

- [ ] Add `harden-runner` as the first step in security-sensitive jobs (release, Docker push)
- [ ] Audit mode initially, block mode once baseline is established

### 3.6 Tighten permissions

- [ ] Set top-level `permissions: {}` (deny all) in every workflow
- [ ] Grant minimum required permissions per-job, not per-workflow
- [ ] `security.yml`: Move `security-events: write` to only the jobs that need it
- [ ] `release.yml`: Split permissions per job instead of broad workflow-level grants

### 3.7 Pin gosec to tagged version

- [ ] Replace `securego/gosec@master` with a pinned SHA of the latest tagged release
- [ ] `@master` is a security risk (mutable reference)

---

## Phase 4: Release Pipeline Improvements

### 4.1 Add cosign keyless signing to releases

- [ ] Install cosign in release workflow via `sigstore/cosign-installer@v3`
- [ ] Sign release checksums file with cosign (keyless via GitHub OIDC)
- [ ] Sign Docker images with cosign after push
- [ ] Add verification instructions to release notes

### 4.2 Add SBOM generation to releases

- [ ] Install Syft via `anchore/sbom-action/download-syft@v0`
- [ ] Generate SBOM for each binary artifact (SPDX or CycloneDX format)
- [ ] Attach SBOMs to GitHub release
- [ ] Enable SBOM attestation on Docker images via `docker/build-push-action` `sbom: true`

### 4.3 Add build provenance attestation to binaries

- [ ] Use `actions/attest-build-provenance@v3` for binary artifacts (not just Docker)
- [ ] Achieves SLSA Build Level 2 for binaries

### 4.4 Add Docker image scanning to release pipeline

- [ ] Scan release Docker images with Trivy before publishing
- [ ] Fail the release if CRITICAL vulnerabilities are found in the image

### 4.5 Add agent Docker build to release pipeline

- [ ] Verify `release.yml` `build-docker` job builds both gearbox and gearbox-agent images
- [ ] Currently only builds dashboard image; add parallel job for agent image

---

## Phase 5: Monthly Automation

### 5.1 Create monthly release workflow

- [ ] New workflow: `monthly-release.yml`
- [ ] Trigger: `schedule: cron '0 6 1 * *'` (1st of month, 6 AM UTC) + `workflow_dispatch`
- [ ] Steps:
  1. Check out main branch
  2. Determine next version (read latest tag, auto-increment patch)
  3. Create a PR that bumps the version (if version file exists) or just creates a tag
  4. Build Docker images with `--no-cache` to pull latest Alpine packages (`apk upgrade`)
  5. Cross-compile binaries for all platforms
  6. Run Trivy scan on fresh images
  7. Generate checksums and SBOM
  8. Sign with cosign
  9. Create GitHub release with changelog noting "Monthly automated release"
  10. Push Docker images with version + `monthly-YYYYMM` tags

### 5.2 Add version auto-increment logic

- [ ] Script to determine next patch version from latest git tag
- [ ] Format: if latest is `v1.2.3`, next monthly is `v1.2.4`
- [ ] Manual releases can still use any version via tag push
- [ ] Include `workflow_dispatch` input to override version if needed

### 5.3 Monthly release changelog

- [ ] Auto-generate changelog from commits since last release
- [ ] Include a note that this is an automated monthly release with updated system packages
- [ ] List any dependency updates that were merged since last release

---

## Phase 6: Dependency Management

### 6.1 Remove Renovate configuration

- [ ] Delete `.github/renovate.json`
- [ ] Dependabot will be the sole dependency manager (native GitHub, simpler)

### 6.2 Update Dependabot configuration

- [ ] Add `gearbox-agent/` Go modules tracking (currently only `gearbox/`)
- [ ] Add `gearbox-agent/` Docker base image tracking (currently only `gearbox/`)
- [ ] Ensure all groups use `patterns: ["*"]` to bundle into single PRs
- [ ] Add `npm` ecosystem for `gearbox/` (if `package.json` exists)
- [ ] Set consistent schedule across all ecosystems

Updated config should track:

| Ecosystem | Directories | Group |
|-----------|------------|-------|
| `gomod` | `/gearbox`, `/gearbox-agent` | All Go deps in one PR |
| `github-actions` | `/` | All Actions in one PR |
| `docker` | `/gearbox`, `/gearbox-agent` | All Docker in one PR |

### 6.3 Create Dependabot auto-merge workflow

- [ ] New workflow: `dependabot-auto-merge.yml`
- [ ] Auto-merge patch and minor dependency updates after CI passes
- [ ] Require manual review for major version bumps
- [ ] Use `dependabot/fetch-metadata@v2` to check update type
- [ ] Use `gh pr merge --auto --squash` for approved PRs
- [ ] This means monthly you get ~3 grouped PRs max (Go deps, Actions, Docker) that auto-merge

### 6.4 Bundled monthly dependency PR strategy

The combination of:

1. Dependabot **grouped updates** (`patterns: ["*"]`) - bundles all deps per ecosystem into one PR
2. Dependabot **monthly schedule** - only runs once per month
3. **Auto-merge workflow** - auto-merges patch/minor after CI passes

Results in: **At most 3 PRs per month** (Go, Actions, Docker), most auto-merging. Major bumps require one manual review. Then the monthly release picks up all merged changes.

---

## Phase 7: New Workflows

### 7.1 Add OpenSSF Scorecard workflow

- [ ] New workflow: `scorecard.yml`
- [ ] Uses `ossf/scorecard-action@v2`
- [ ] Runs weekly on main branch
- [ ] Publishes results to GitHub Security tab
- [ ] Provides public supply chain security score

### 7.2 Add PR labeler workflow

- [ ] New workflow or add to existing: auto-label PRs based on changed paths
- [ ] Labels: `dashboard`, `agent`, `docs`, `ci`, `dependencies`
- [ ] Helps with changelog generation and filtering

### 7.3 Add stale issue/PR workflow (optional)

- [ ] Auto-close stale issues/PRs after configurable period
- [ ] Low priority; skip if not desired

---

## Phase 8: Deletions

### 8.1 Remove Renovate

- [ ] Delete `.github/renovate.json`

### 8.2 Clean up duplicate Docker builds in release

- [ ] `release.yml` has a `build-docker` job that duplicates what `docker.yml` does on tag push
- [ ] `docker.yml` already triggers on `v*` tags and builds with proper semver tags
- [ ] **Option A**: Remove `build-docker` from `release.yml` entirely (let `docker.yml` + `docker-agent.yml` handle it)
- [ ] **Option B**: Keep in `release.yml` but disable `docker.yml` tag trigger to avoid double builds
- [ ] Recommended: **Option A** - cleaner separation of concerns

---

## Reference: Current vs Target State

### Workflow Files

| File | Current | Target |
|------|---------|--------|
| `ci.yml` | Dashboard CI (haproxy-monitor naming) | Dashboard CI (gearbox naming, go-version-file, path filters) |
| `ci-agent.yml` | Agent CI | Agent CI (go-version-file, pinned tools) |
| `docker.yml` | Dashboard Docker | + Trivy image scan, SHA-pinned actions |
| `docker-agent.yml` | Agent Docker | + Trivy image scan, SHA-pinned actions |
| `release.yml` | Release (haproxy-monitor, no signing) | Release (gearbox, cosign, SBOM, provenance, agent Docker) |
| `security.yml` | Security (codeql v3, gosec@master) | Security (codeql v4, pinned gosec, Docker image scan) |
| `codeql.yml` | **Does not exist** | CodeQL deep static analysis |
| `monthly-release.yml` | **Does not exist** | Monthly automated release with fresh packages |
| `dependabot-auto-merge.yml` | **Does not exist** | Auto-merge minor/patch dep updates |
| `scorecard.yml` | **Does not exist** | OpenSSF Scorecard |
| `dependabot.yml` | Missing agent dirs, no npm | Full coverage: gomod x2, actions, docker x2 |
| `renovate.json` | Exists (conflicts with Dependabot) | **Delete** |

### Action Versions

| Action | Current | Target |
|--------|---------|--------|
| `actions/checkout` | `@v6` | `@<sha>` (pin to v6 SHA) |
| `actions/setup-go` | `@v6` | `@<sha>` (pin to v6 SHA) + `go-version-file` |
| `actions/upload-artifact` | `@v6` | `@<sha>` (pin to v6 SHA) |
| `github/codeql-action/upload-sarif` | `@v3` | `@<sha>` (pin to **v4** SHA) |
| `golangci/golangci-lint-action` | `@v9` | `@<sha>` (pin to v9 SHA) + pinned lint version |
| `securego/gosec` | `@master` | `@<sha>` (pin to latest tagged release SHA) |
| `aquasecurity/trivy-action` | `@master` | `@<sha>` (pin to latest release SHA) |
| `codecov/codecov-action` | `@v5` | `@<sha>` (pin to v5 SHA) |
| `docker/build-push-action` | `@v6` | `@<sha>` (pin to v6 SHA) |
| `docker/login-action` | `@v3` | `@<sha>` (pin to v3 SHA) |
| `docker/metadata-action` | `@v5` | `@<sha>` (pin to v5 SHA) |
| `docker/setup-buildx-action` | `@v3` | `@<sha>` (pin to v3 SHA) |
| `docker/setup-qemu-action` | `@v3` | `@<sha>` (pin to v3 SHA) |
| `softprops/action-gh-release` | `@v2` | `@<sha>` (pin to v2 SHA) |
| `actions/attest-build-provenance` | `@v3` | `@<sha>` (pin to v3 SHA) |
| `gitleaks/gitleaks-action` | `@v2` | `@<sha>` (pin to v2 SHA) |
| `actions/dependency-review-action` | `@v6` | `@<sha>` (pin to v6 SHA) |
| `sigstore/cosign-installer` | **Not used** | Add `@<sha>` (v3 SHA) |
| `anchore/sbom-action` | **Not used** | Add `@<sha>` (v0 SHA) |
| `ossf/scorecard-action` | **Not used** | Add `@<sha>` (v2 SHA) |
| `step-security/harden-runner` | **Not used** | Add `@<sha>` (v2 SHA) |

### Dependency Management

| Aspect | Current | Target |
|--------|---------|--------|
| Managers | Dependabot + Renovate (conflict) | Dependabot only |
| Go modules | gearbox/ only | gearbox/ + gearbox-agent/ |
| Docker | gearbox/ only | gearbox/ + gearbox-agent/ |
| Grouping | Partial | Full (`patterns: ["*"]` on all) |
| Auto-merge | Via Renovate (to be removed) | Via `dependabot-auto-merge.yml` workflow |
| Schedule | Monthly | Monthly |
| Max PRs/month | Uncontrolled | ~3 (Go, Actions, Docker), mostly auto-merged |

---

## Implementation Priority

**High priority (do first):**

1. Phase 1 - Cleanup stale naming and fix `go-version-file` (correctness issues)
2. Phase 6.1 - Remove Renovate (stop duplicate PRs immediately)
3. Phase 3.1 - Upgrade codeql-action to v4 (v3 deprecation deadline)

**Medium priority:**

4. Phase 6.2-6.4 - Dependabot updates + auto-merge workflow
5. Phase 3.2-3.7 - Security hardening (SHA pinning, harden-runner, permissions)
6. Phase 2 - CI improvements (path filters, templ check)
7. Phase 8 - Clean up duplicate Docker builds

**Lower priority (do after core is solid):**

8. Phase 4 - Release pipeline (cosign, SBOM, provenance)
9. Phase 5 - Monthly automation
10. Phase 7 - New workflows (Scorecard, labeler)
