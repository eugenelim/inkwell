---
name: mail-triage
description: Use this skill when the user wants their actual mailbox triaged, organized, or processed via the inkwell CLI — acting as an executive assistant on live mail, not developing inkwell. Triggers on "triage", "sort my inbox", "organize my email", "process my inbox", "clear my inbox", "clean up my email", "deal with the screener queue". Makes one classification pass and files every inbox thread into the workflow folders (@Action / @Waiting / Archive / Reference / Newsletters) or soft-deletes the worthless — behind a presented list and explicit confirmation. Do NOT use for inkwell code changes (use `work-loop` / `bug-fix`), for read-only briefings (use `mail-executive-summary`), or for writing replies (use `mail-draft-reply`).
---

# Skill: mail-triage

Make one classification pass over the inbox and file each thread.
The inbox is a **processing queue, not storage** — it should end up
empty: what needs the user lives in `@Action`, what they're waiting
on in `@Waiting`, everything else is filed where search can find it
or soft-deleted. Folders are **workflow-based, not topic-based**:
they encode whose turn it is, not what the mail is about (topics get
additive categories instead — see the rubric).

**This skill is the single writer of mailbox filing state.** The
other mail-* skills READ the folders this skill maintains
(`mail-executive-summary` reads @Action/@Waiting, `mail-follow-up`
reads @Waiting, `mail-draft-reply` works the @Action queue) — don't
duplicate classification elsewhere.

## The fixed structure (never invent new folders)

| Folder | Purpose |
| --- | --- |
| `@Action` | Emails the user needs to act on |
| `@Waiting` | User replied/delegated; waiting on someone else |
| `Archive` | The "done" pile — everything processed, kept for search (built-in) |
| `Reference` *(optional)* | Non-actionable keepers: receipts, finance, important docs |
| `Newsletters` *(optional)* | Bulk the user has explicitly said to keep — skips the inbox |

Plus **soft delete** (→ Deleted Items, recoverable) — the **default
disposition for bulk and irrelevant mail**; filing bulk into
`Newsletters` is the opt-in exception, per hard rule 2 below.

## Hard rules (non-negotiable)

1. **inkwell cannot send mail.** `Mail.Send` is a denied scope by
   construction (PRD §3.1). Nothing in this skill sends anything.
2. **Soft delete is the removal verb for worthless mail.**
   `--action delete` / `messages delete` move to Deleted Items —
   recoverable until the user empties it. Mail with *any* search
   value is filed (Archive / Reference / Newsletters) instead.
   `permanent-delete` never, unless the user literally says
   "permanently" in this conversation — then confirm once more.
3. **Present the list before moving or removing.** Before any batch
   files or deletes, show the user the *actual messages* —
   `date · from · subject → destination`, one line each (grouped by
   verdict) — never just counts. Soft-delete rows called out
   separately from filing rows.
4. **Confirmation lives in chat, not in the prompt.** Tool shells
   have no TTY — interactive prompts hang. Get the user's explicit
   "yes" on the presented plan first, then run with `--yes` where a
   command needs it. Never pass `--yes` speculatively.
5. **Never create folders beyond the fixed set**, and never
   auto-create project categories (rubric: closed-set,
   abstain-first).
6. **When in doubt, file conservatively.** Unsure between @Action
   and anything else → @Action. Unsure between filing and deleting →
   file. A misfiled keeper beats a deleted obligation.
7. **Report what actually happened** — per-verb succeeded/failed
   counts from command output, not what was intended.

## Procedure

1. **Preflight + sync.** `python3 scripts/preflight.py` (relative to
   this skill's directory; fails clean — relay its instructions),
   then `inkwell sync` once. On sync failure: say so; proceed only
   with an explicit staleness caveat.

2. **Check the structure — offer to create what's missing.**
   `scripts/survey.py` reports `folder_setup` (which workflow folders
   exist; `Archive` is built-in). If `@Action`/`@Waiting` (or the
   optional ones the plan needs) are missing, **offer** — don't
   assume:

   > "The workflow folders aren't set up yet. Create `@Action` and
   > `@Waiting` (and optionally `Reference`, `Newsletters`)?"

   On yes: `inkwell folder new "@Action"` etc. On no: triage in
   degraded mode — keep needs-you mail in the Inbox, still
   Archive/soft-delete the rest — and note the limitation.

3. **Survey.** `python3 scripts/survey.py` pulls a batch (default:
   unread Inbox, capped at 100 — triage in passes; `--include-read
   --days N` for deeper sweeps), dedupes to latest-per-thread, and
   annotates mechanical signals (`keep_rails`, `archive_hints`) plus
   `folder_setup`. `--format table` prints the presentation shape
   hard rule 3 requires.

4. **Classify — one verdict per thread.** Read
   [`references/rubric.md`](references/rubric.md) and assign exactly
   one verdict per thread, deciding whose turn it is across the
   WHOLE thread, not just the last message. Read full bodies
   (`inkwell messages show <id>`) only where the rubric says the
   envelope is ambiguous. Optional project categories apply on top —
   closed set, abstain-first (rubric §projects). No mailbox writes
   during classification.

5. **Present the plan** grouped by verdict, every row as
   `date · from · subject → destination`, soft-deletes flagged.
   Sanity brake: if > ~80% of the batch would be soft-deleted, say
   so and re-check the rubric before asking for the go-ahead.

6. **Execute** only the approved rows:

   | Verdict → destination | Command |
   | --- | --- |
   | To Reply / needs action → `@Action` | `inkwell thread move <conversation-id> --to "@Action"` (single msg: `messages move <id> --to "@Action"`) |
   | Awaiting reply → `@Waiting` | `inkwell thread move <conversation-id> --to "@Waiting"` |
   | Done / handled → `Archive` | `inkwell thread archive <conversation-id>`; bulk: `inkwell filter '<pattern>' --action archive --apply --yes` |
   | Receipt / record → `Reference` | `inkwell messages move <id> --to Reference` |
   | Bulk (newsletters, marketing, notifications, cold) → **soft delete (default)** | `inkwell filter '<pattern>' --action delete --apply --yes`; single: `messages delete <id>` |
   | Bulk the user explicitly keeps → `Newsletters` | `inkwell messages move <id> --to Newsletters` |
   | Project tag (approved list only) | `inkwell action run tag_<project> --message <id>` (one-time `actions.toml` setup per project — rubric §projects) |
   | Recurring sender → durable fix | `inkwell route assign <addr> feed\|paper_trail` / `screener accept\|reject <addr>` / server-side rule (`rules apply --dry-run` → confirm → `apply`) |

7. **Verify & report.** Per-verb succeeded/failed counts; the inbox
   query that should now be empty (or near it); counts per verdict;
   any proposed-but-unapproved project categories with examples; and
   the residue left untouched and why.

## Pattern language (spec 08)

Full grammar: `docs/user/reference.md` § "Pattern operators". Core:
`~f addr` from (wildcards `newsletter@*`) · `~s/~b/~B`
subject/body/either · `~d <30d` date · `~N` unread / `~U` read (mind
the inversion) · `~F` flagged · `~A` attachments · `~G cat` category
· `~o feed` routing dest · `~v conv-id` thread · `~m folder`. Quote
the whole pattern in shell.

## Anti-patterns to refuse

- **Topic folders.** "Vendors", "Project X", "Travel" — that's what
  categories and search are for. Folders encode workflow state only.
- **Removing or filing on vibes.** No batch without its presented
  list in this conversation.
- **Counts instead of messages** — the user approves a list, not a
  number.
- **Creating folders beyond the fixed set** or auto-creating project
  categories.
- **Touching the `Inkwell/*` categories** (ReplyLater / SetAside are
  the stacks' storage — managed via `later` / `aside`, never raw).
- **Skipping the rubric order.** Keep-rails run before any removal.
- **`permanent-delete` as a convenience.** Hard rule 2.
- **Triaging stale cache silently.**
