# Spec 20 — Cross-folder bulk operations

**Status:** Ready for implementation.
**Depends on:** Specs 08 (pattern compile), 09 (batch executor), 10
(filter UX).
**Blocks:** Custom actions framework (§2 — `--all-folders` semantics
get reused).
**Estimated effort:** 1 day.

---

## 1. Goal

Today `:filter ~f newsletter@*` only matches messages in the
currently-open folder. The pattern language naturally describes
"every message that matches X" — there's no good reason to scope
that to one folder. This spec extends `:filter` (and the
`inkwell filter` CLI from spec 14) to optionally search every
subscribed folder at once.

The natural use case: "delete every newsletter older than 30
days, no matter what folder it's filed in." Today the user has to
run the filter once per folder.

## 2. Prior art

- **mutt / neomutt** — virtual folders via notmuch or
  `imap_pipeline_depth` workarounds. Not native.
- **aerc** — single-folder filter only; the `:filter` command is
  per-mailbox.
- **alot (notmuch)** — natively cross-folder. Tags exist outside
  of folders; queries always span the whole index.
- **Gmail** — search is implicitly cross-label by default; users
  click "this label" to scope.
- **Outlook web** — search has a folder dropdown; default is
  "Current mailbox" (every folder).

We follow Outlook's default-cross-folder model with an explicit
flag to scope down. `:filter` stays current-folder by default to
preserve muscle memory; `:filter --all` (or `:filter -a`) goes
mailbox-wide.

## 3. UI

### 3.1 New flag in `:filter`

```
:filter ~f newsletter@*           — current folder (today's behaviour)
:filter -a ~f newsletter@*        — every subscribed folder
:filter --all ~f newsletter@*     — same
```

The cmd-bar reminder distinguishes the two:

```
filter: ~f newsletter@* · matched 47 (Inbox) · ;d delete · :unfilter
filter: ~f newsletter@* · matched 247 (5 folders) · ;d delete · :unfilter
```

### 3.2 `;d` / `;a` semantics across folders

Bulk dispatch fires the same way; the IDs come from across folders.
Spec 09's BatchExecute is folder-agnostic. Confirm-pane copy:

```
Delete 247 messages across 5 folders? [y/N]
```

Folder count is for clarity; the user knows they're touching more
than one folder.

### 3.3 Indicator in result rows

When a cross-folder filter is active, the list pane shows the
folder name in a new column on each row:

```
RECEIVED          FROM             FOLDER       SUBJECT
Mon 14:30         Alice            Inbox        Q4 deck
Mon 13:55         Bob              Clients      Re: deck
…
```

Single-folder filter view (today) doesn't render the folder column —
the column appears only when the result set spans more than one
folder. UI auto-detects from the result.

## 4. Implementation

The pattern compile path (spec 08 → CompileLocal) already produces
a SQL WHERE clause + args. Today's caller path is:

```go
store.SearchByPredicate(accID, where, args, limit)
```

which has implicit `account_id = ?` scoping but no folder scope.

Cross-folder is the **default** behaviour of SearchByPredicate
already — it just queries across `messages`, no folder filter. The
single-folder path adds `AND folder_id = ?`.

So this spec is mostly a UI / CLI flag plumbing exercise:

1. Parse `--all` / `-a` from the cmd-bar input.
2. When set, drop the implicit `folder_id = m.list.FolderID`
   condition that the current `runFilterCmd` adds.
3. Render the folder column when result spans >1 folder.

CLI:

```sh
inkwell filter '~f newsletter@*' --all --action delete --apply
inkwell messages --filter '~f newsletter@*' --all --limit 100
```

`--all` on `inkwell messages` similarly drops the `--folder` scope.

## 5. Performance

A pattern over the entire mailbox hits the same FTS5 / message-
index path. Spec 02's bench gates `Search(q, limit=50)` over 100k
msgs at <100ms p95; cross-folder is the same query without a
folder filter — should be faster, not slower (one fewer WHERE
clause).

If we observe slow cross-folder queries on huge mailboxes, the
fix is a covering index on `(account_id, received_at)` — already
implied by the existing `idx_messages_received` (spec 02).

## 6. Edge cases

| Case                                              | Behaviour                                                          |
| ------------------------------------------------- | ------------------------------------------------------------------ |
| `:filter --all` with no pattern after the flag    | Friendly error: `:filter --all <pattern>`.                         |
| Pattern is folder-scoped (`~m Inbox`) AND `--all` | Pattern wins; folder filter inside the pattern still applies.       |
| User presses `;d` on a cross-folder result        | Confirm modal counts folders too. Same destructive default-N.        |
| Cross-folder result includes subscribed Junk      | Surface the folder so user spots junk-only matches before deleting. |
| Result is empty                                   | Same as today: status bar shows `0 matched`.                        |

## 7. Definition of done

- [ ] `:filter --all <pattern>` parses and runs against every folder.
      `:filter -a` is the short form.
- [ ] `inkwell filter --all <pattern>` CLI parity.
- [ ] `inkwell messages --filter <pattern> --all` parity.
- [ ] Cmd-bar reminder shows folder count when matches span >1
      folder.
- [ ] List pane renders FOLDER column when result spans >1 folder;
      hides it for single-folder results.
- [ ] Confirm modal for `;d` / `;a` includes folder count.
- [ ] Tests: dispatch (`--all` flag flips the right boolean on
      Model); store query verifies no folder_id condition; e2e
      drives `:filter --all <pattern>` against a fixture spanning
      two folders and asserts both folders' messages appear.
- [ ] User docs: reference (`:filter --all` row); how-to ("Cross-
      folder cleanup" recipe).

## 8. Cross-cutting checklist

- [ ] Scopes: none new.
- [ ] Store reads/writes: messages (read).
- [ ] Graph endpoints: spec 09's $batch on apply path; otherwise
      none.
- [ ] Offline: works fully offline against the local store.
- [ ] Undo: per-message via spec 07.
- [ ] User errors: §6 table.
- [ ] Tests: §7.
