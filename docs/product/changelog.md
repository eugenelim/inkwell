# Changelog

User-visible changes per release, in [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
format. Updated in the same PR as any user-visible behaviour change
(`docs/CONVENTIONS.md` §10 / §12.6).

Conventions:

- **Added** for new features.
- **Changed** for changes in existing functionality.
- **Deprecated** for soon-to-be removed features.
- **Removed** for now-removed features.
- **Fixed** for any bug fixes.
- **Security** for vulnerabilities.

For the per-spec implementation log + perf measurements, see the
spec's `plan.md` (e.g. `docs/specs/35-body-regex-local-indexing/plan.md`).
For long-form release notes, see GitHub Releases.

## [Unreleased]

### Added

- Opt-in local body indexing — `[body_index].enabled = true`
  decodes message bodies into a parallel `body_text` table plus
  two FTS5 surfaces (unicode61 for tokens, trigram for substring
  / regex narrowing). Pattern language now admits regex on
  `~s` / `~b` / `~B` (`/.../` delimiter, `\/` escape) when the
  index is enabled. New `inkwell index {status, rebuild, evict,
  disable}` CLI surfaces the size, caps, and destructive ops.
  Default off; bounded by configurable count + bytes caps (default
  5000 messages / 500 MB); `inkwell index disable` purges
  everything. No new Graph scopes. (spec 35, ongoing — UI surfaces
  in a follow-up iteration)

## [0.63.0] — 2026-05-14

### Added

- Calendar invites read + hand-off in the viewer pane. When the
  focused message is a meeting invite, a compact card renders above
  the body (subject, when, where, organizer, RSVP counts, current
  response). `o` opens the event in Outlook on the web for the
  two-click RSVP path. (spec 34)

## [0.62.0] — 2026-05-13

### Added

- Markdown drafts. Opt-in via `[compose] body_format = "markdown"`;
  inkwell converts via goldmark (CommonMark + GFM) before saving on
  Graph. `Ctrl+E` writes `.md` tempfiles so `$EDITOR` detects
  filetype. Default remains `"plain"`. (spec 33)

## [0.61.0] — 2026-05-12

### Added

- Server-side rules — `inkwell rules` CLI, cmd-bar `:rules`, palette
  rows. Terraform-style `rules.toml` + pull / apply workflow. Curated
  subset of Outlook's predicate / action fields (denied scopes
  excluded; non-curated fields preserved as `additionalData` for
  round-trip). Modal manager deferred. (spec 32)

## [0.60.0] — 2026-05-12

### Added

- Focused / Other tab in the list pane, backed by
  `inferenceClassification`. (spec 31)

## Earlier releases

Per-release notes from v0.1.0 → v0.59.0 are not retroactively
back-filled here. The shipped-spec rows in
[`docs/PRD.md §10`](../PRD.md) and
[`docs/product/roadmap.md`](roadmap.md) are the authoritative
per-version inventory; the spec's own `**Shipped:** vX.Y.Z` line
under `docs/specs/NN-<title>/spec.md` is the ground truth for
which version a feature first appeared in. GitHub Releases carries
the long-form notes.

This changelog is the canonical surface going forward (from v0.64
onward, every user-visible release lands a Keep-a-Changelog entry
in this file in the same PR per `docs/CONVENTIONS.md` §12.6).

[Unreleased]: https://github.com/eugenelim/inkwell/compare/v0.63.0...HEAD
[0.63.0]: https://github.com/eugenelim/inkwell/releases/tag/v0.63.0
[0.62.0]: https://github.com/eugenelim/inkwell/releases/tag/v0.62.0
[0.61.0]: https://github.com/eugenelim/inkwell/releases/tag/v0.61.0
[0.60.0]: https://github.com/eugenelim/inkwell/releases/tag/v0.60.0
