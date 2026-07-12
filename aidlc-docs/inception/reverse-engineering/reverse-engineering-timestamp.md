# Reverse Engineering Metadata

**Analysis Date**: 2026-07-08T06:11:53Z
**Analyzer**: AI-DLC
**Workspace**: /home/thinkpad/Desktop/src/Service-Nuclei-Worker
**Total Files Analyzed**: 40 non-test Go source files (~4,900 LOC excl. vendor & generated docs), plus Dockerfiles, Makefile, Helm chart, and README.

## Artifacts Generated
- [x] business-overview.md
- [x] architecture.md
- [x] code-structure.md
- [x] api-documentation.md
- [x] component-inventory.md
- [x] technology-stack.md
- [x] dependencies.md
- [x] code-quality-assessment.md

## Supplement — 2026-07-08 (targeted)
**Trigger**: implementation intent from `cwe-eval-report.html`; baseline artifacts did not cover engine coupling.
- [x] nuclei-coupling-analysis.md — full non-vendor scan (20 Go files importing `projectdiscovery/*`, 39 files mentioning nuclei, 7 uncoupled packages)
