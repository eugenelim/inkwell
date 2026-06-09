# How-to: set up the AI assistant skills

The repo ships a pack of Claude Code skills that drive your live
mailbox through the `inkwell` CLI, acting as an executive assistant.

## The philosophy: an executive assistant working toward inbox zero

A good EA doesn't read your mail *to* you — they make sure the only
things in front of you are the things that need **you**. The skill
pack treats inbox zero as a workflow, not a neurosis: the inbox is a
to-do list of other people's asks, and everything else is either
delegated to a standing rule, parked where it's searchable, or
soft-deleted.

Four principles shape the pack:

1. **Staged trust.** Skills are split by blast radius — *read-only*
   (inform), *draft* (prepare, never send), *mutate* (change mailbox
   state, only behind a presented list and your explicit yes). Each
   skill refuses the next stage's actions and hands off instead; a
   briefing will *offer* to triage, never just do it. Sending mail is
   above all three stages: `Mail.Send` is denied by construction
   (PRD §3.1), so the ceiling is a draft in your Drafts folder.
2. **Deterministic mechanics, reviewable judgment.** Every skill
   pairs Python scripts (the reproducible part: gathering, deduping,
   ranking, saving) with written rubrics the model applies (the
   judgment part: what needs a reply, what's noise). You can audit
   both — the scripts don't decide, and the decisions are on paper.
3. **Workflow folders, not topic folders.** The inbox is a
   processing queue, not storage — after triage it's empty. Mail is
   filed by *whose turn it is*, never by what it's about (topics get
   additive categories and search):

   | Folder | Purpose |
   | --- | --- |
   | `@Action` | Emails you need to act on |
   | `@Waiting` | You've replied/delegated and are waiting on someone else |
   | `Archive` | The "done" pile — everything processed, kept for search |
   | `Reference` *(optional)* | Non-actionable keepers: receipts, finance, important docs |
   | `Newsletters` *(optional)* | Bulk mail you've chosen to keep, skipping the inbox |

   Bulk you *haven't* chosen to keep is **soft-deleted** (Deleted
   Items — recoverable), not filed. `mail-triage` is the **single
   writer** of this structure (it offers to create the folders on
   first run); the other skills read it — the brief reads
   `@Action`/`@Waiting` as pre-classified state, follow-up chases
   `@Waiting` — and every one of them falls back to working straight
   from the inbox if you choose not to run triage.

4. **Fix the source, not just the pile.** One-off cleanup loses to
   recurring noise. The loop that actually reaches — and keeps —
   inbox zero: *brief* (know what needs you) → *draft / follow-up*
   (clear what you owe and chase what you're owed) → *triage*
   (soft-delete the irrelevant, file the rest into the workflow
   folders) → *reduce-noise / reclaim-storage* (stop the refill,
   recover the space) → repeat.

## The pack

| Skill | Stage | What it does |
| --- | --- | --- |
| `mail-executive-summary` | read-only | Morning briefing: needs-reply / waiting / FYI / handled, plus calendar. Never modifies mail. |
| `mail-meeting-prep` | read-only | Briefs you on the external guests in today's meetings — recent discussions, outstanding items. |
| `mail-draft-reply` | draft | Drafts replies / forwards in your voice, saved to the Drafts folder. Never sends; you send from Outlook. |
| `mail-follow-up` | draft | Finds threads gone quiet where you sent the last message, drafts polite nudges for approval. |
| `mail-triage` | mutate | One pass over the inbox: files every thread into the workflow folders (@Action / @Waiting / Archive / Reference / Newsletters) or soft-deletes the worthless — behind a presented list and your explicit confirmation. Offers to create the folders on first run. |
| `mail-reduce-noise` | mutate | Sender-level: ranks noisy senders, proposes unsubscribe (you execute in the TUI) or standing auto-routing. |
| `mail-reclaim-storage` | mutate | Frees quota: soft-deletes large old attachments and bulk back-catalogs to Deleted Items; never empties it. |

The skill files live in [`.claude/skills/`](../../../.claude/skills/).

## The routine: what to run when

How an EA actually runs a principal's inbox — a few minutes daily,
deeper passes on a slower cadence. Order matters: know the day before
you act on it, clear what you owe before chasing what you're owed,
and fix sources before symptoms.

**Every morning (~5 minutes):**

1. `mail-executive-summary` — the brief. Know what needs you, what
   you're waiting on, and what today's calendar looks like before
   touching anything.
2. `mail-meeting-prep` — for today's external meetings, right after
   the brief (or the evening before a packed morning).
3. `mail-draft-reply` — work the brief's "needs your reply" list,
   highest rank first. Review the drafts, send from Outlook.
4. `mail-triage` — sweep the residue to zero: soft-delete the
   irrelevant, file the rest, stack what can wait.

**End of day (optional, ~2 minutes):** a second `mail-triage` sweep
of what arrived since the morning, so tomorrow's brief starts clean.

**Weekly (pick a fixed day — Friday close or Monday start):**

5. `mail-follow-up` — chase the threads that went quiet this week
   (the 3-business-day default means a weekly run catches everything
   worth nudging without pestering anyone).
6. A deeper `mail-triage` pass over the week's read-but-kept mail
   (`--days 7 --include-read`).

**Monthly (or whenever the daily sweep starts feeling heavy):**

7. `mail-reduce-noise` — if the same senders keep showing up in the
   triage delete-list, stop them at the source instead.

**Quarterly (or on a quota warning):**

8. `mail-reclaim-storage` — large old attachments and bulk
   back-catalogs; then empty Deleted Items yourself.

Only the read-only steps (1–2) are sensible candidates for unattended
scheduling (e.g. a morning cron posting the brief). Everything from
step 3 down is interactive by design — drafts need your review, and
the mutate-stage skills require your confirmation on a presented
list, every time.

## 1. Put inkwell on your PATH

The skills (and any shell script or cron job) locate the binary as:
`$INKWELL_BIN` → `inkwell` on PATH → `bin/inkwell` in the repo.

**Option A — release binary** (no Go toolchain needed): follow
[tutorial → Step 1](../tutorial.md#step-1--install); the `sudo mv
/tmp/inkwell /usr/local/bin/` step is the PATH install
(`/usr/local/bin` is on the default macOS PATH).

**Option B — from a source checkout:**

```sh
# From the repo root:
go install ./cmd/inkwell          # installs to $(go env GOPATH)/bin
# …or build in place and symlink:
go build -o bin/inkwell ./cmd/inkwell
mkdir -p ~/.local/bin && ln -sf "$PWD/bin/inkwell" ~/.local/bin/inkwell
```

If the target directory isn't already on your PATH, add it to
`~/.zshrc` (macOS default shell):

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc        # Option B symlink
echo 'export PATH="$(go env GOPATH)/bin:$PATH"' >> ~/.zshrc    # go install
source ~/.zshrc
```

**Option C — pin a specific binary** without touching PATH:

```sh
export INKWELL_BIN=/path/to/inkwell
```

## 2. Sign in once, interactively

The skills can read and act on mail, but only a human can complete
the browser sign-in:

```sh
command -v inkwell      # prints the resolved path
inkwell --version
inkwell signin          # once; tokens live in Keychain ~90 days
inkwell whoami          # confirms the skills will find a session
```

## 3. Verify the skills can find it

Each skill starts with a preflight that checks exactly this and fails
clean with instructions (it never installs anything):

```sh
python3 .claude/skills/mail-triage/scripts/preflight.py
```

Exit 0 means you're set. Exit 1 → binary missing (this guide,
step 1). Exit 2 → not signed in (step 2).

If `inkwell` is *not* on PATH, the skills fall back to building
`bin/inkwell` inside the repo checkout — slower on first use, and it
only works when the session is rooted in the inkwell repo.

## 4. Use them

In a Claude Code session in this repo, just ask:

- "Brief me on my email" → `mail-executive-summary`
- "Who am I meeting today?" → `mail-meeting-prep`
- "Draft a reply to Alice's budget mail" → `mail-draft-reply`
- "What am I waiting on? Chase it." → `mail-follow-up`
- "Clean up my inbox" → `mail-triage`
- "Stop the newsletters cluttering my inbox" → `mail-reduce-noise`
- "My mailbox is full, clear some space" → `mail-reclaim-storage`

Safety expectations: the assistant shows you every message it
proposes to archive or delete before acting, asks before anything
durable (routing, rules, OOO), and can never send mail.
