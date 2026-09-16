# Changelog

All notable changes to IntellyRouter are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Direct model names through provider slug (`<slug>/<model id>`).
- Combo tiers (`combo/<name>`) with fallback, round-robin, and
  least-used strategies.
- Advisor model column on the Sessions list and KPI on the session
  detail page.
- Image-aware tier selection per request, with a fallback to lower
  tiers that read images.
- Saved and Sub-value columns on the Sessions list.
- Director consult vs. director step distinction in the ledger.
- Combo vision flag, derived from members.
- Multi-stage Dockerfile, Coolify / Dokploy compose file.

### Changed
- Default comparator on the session detail page is the session's
  director model.
- Sessions endpoint work totals exclude routing overhead (classifier,
  advisor, director consults).

### Fixed
- Image request now drops down to a tier that accepts images when no
  higher tier does.
- Combo tier takes images when any member does.
- Subscription token no longer used on gateway-originated calls.
- Director guidance delivery does not ask the executor to restart
  finished steps.
