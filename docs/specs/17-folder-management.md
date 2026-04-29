# Spec 17 — Folder management (create / rename / delete)

**Status:** Ready for implementation.
**Depends on:** Specs 02 (folders table), 03 (sync), 04 (TUI sidebar),
14 (CLI scaffolding).
**Blocks:** Routing destinations (1.9 / Bucket 2 #2) — the routing UX
needs the user to be able to make per-routing folders. Custom actions
(§2) — the `move` op pre-supposes target folders the user can create.
**Estimated effort:** 1 day.

---

## 1. Goal

Let users create, rename, and delete mail folders without leaving
inkwell. Microsoft Graph supports it; we just don't expose it. This
is table-stakes for an inbox client.

## 2. Prior art

- **mutt / neomutt** — `:mailbox` model is read-only; folder
  creation is delegated to the IMAP server's tooling (`fdm`,
  `imapfilter`, etc.). No first-class create UI.
- **aerc** — `:mkdir <name>` creates; `:rmdir` deletes. Works on
  any backend; for IMAP this maps to LIST/CREATE/DELETE.
- **alot (notmuch)** — folders aren't a thing in notmuch (tags
  instead).
- **claws-mail (GUI)** — right-click → New Folder / Rename /
  Delete. Confirmation modal on delete.
- **himalaya** — no folder management.

We follow aerc's command-mode pattern (`:mkdir`, `:rmdir`,
`:mvdir`) plus a sidebar contextual key (`N` new, `R` rename,
`X` delete) for the discoverable form.

## 3. Module layout

```
internal/graph/
└── folders.go         # add CreateFolder / RenameFolder / DeleteFolder

internal/store/
└── folders.go         # propagate ID changes (rename keeps id, delete
                       # cascades messages — already supported via FK)

internal/ui/
├── folders_actions.go # the three new dispatch handlers
└── folder_modal.go    # tiny "name this folder" / "rename to" prompt
```

## 4. Graph endpoints

| Operation | Method + URL                                                              | Notes                                                                                  |
| --------- | ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| Create    | `POST /me/mailFolders` (top-level) or `POST /me/mailFolders/{id}/childFolders` (nested) | Body: `{"displayName": "..."}`. Returns the new folder envelope; we upsert it locally. |
| Rename    | `PATCH /me/mailFolders/{id}`                                              | Body: `{"displayName": "..."}`. The id stays the same.                                  |
| Delete    | `DELETE /me/mailFolders/{id}`                                             | Server-side cascade: child folders + their messages move to Deleted Items recursively. |

Well-known folders (Inbox, Sent Items, Drafts, Archive, etc.) reject
both rename and delete with HTTP 403. We surface the error
unchanged; the user sees it once and learns.

## 5. UI

### 5.1 Sidebar contextual keys (folder pane focused)

| Key  | Action                                                            |
| ---- | ----------------------------------------------------------------- |
| `N`  | Create child folder of the focused folder. Empty input → top-level. |
| `R`  | Rename the focused folder.                                          |
| `X`  | Delete the focused folder. Confirm modal mandatory.                |

`X` is shifted-x (capital) on purpose: matches the `D` permanent-
delete convention, marks it as the destructive variant of `d`
(soft-delete a single message) without overlapping.

### 5.2 Modals

Two tiny prompts:

- **Name modal** (used by `N` and `R`): single-line text input.
  Enter commits, Esc cancels. Pre-filled with the current name on
  rename.
- **Confirm modal** (used by `X`): standard
  "Delete folder 'Vendor Quotes' and 312 messages? [y/N]". Default
  `N`.

The name modal is just a `CommandModel` repurposed — same
keystrokes, different prompt. No new bubbletea component needed.

### 5.3 Optimistic apply

| Action | Local apply | Graph dispatch |
| ------ | ----------- | -------------- |
| Create | Insert into `folders` with a temp `local:<uuid>` ID. Reload sidebar. | Once Graph returns the real ID, replace the temp row in the same tx. |
| Rename | UPDATE the row. Reload sidebar. | If Graph fails, revert. |
| Delete | DELETE the row (FK cascade removes messages). | If Graph fails, the next sync re-creates the folder (server still has it). |

Spec 07's executor already has the optimistic-apply pattern; this
spec extends the action types. Three new entries:

```go
const (
    ActionCreateFolder = "create_folder"
    ActionRenameFolder = "rename_folder"
    ActionDeleteFolder = "delete_folder"
)
```

## 6. CLI

Per spec 14 patterns:

```sh
inkwell folder new "Vendor Quotes"
inkwell folder new "Vendor Quotes/2026"      # nested via slash path
inkwell folder rename "Vendor Quotes" "Vendor"
inkwell folder delete "Vendor Quotes" --yes  # confirms unless --yes
```

`--output json` returns `{id, displayName, parentFolderId}` on
create/rename and `{deleted: true}` on delete.

## 7. Edge cases

| Case                                          | Behaviour                                                            |
| --------------------------------------------- | -------------------------------------------------------------------- |
| Name collision under same parent              | Graph returns `ErrorFolderExists`; surface unchanged.                |
| Well-known folder (Inbox, Drafts, …) on R/X   | Graph returns 403; status: `cannot rename/delete a system folder`.   |
| Folder has nested children                    | Delete prompt counts them too: "Delete 'Clients' (3 children, 1247 messages)?". |
| User in the middle of a `:filter` from this folder | Filter clears on delete (sentinel folder ID); status notes it.       |
| Create with empty name                        | Local validation; never hits Graph.                                  |

## 8. Definition of done

- [ ] `internal/graph/folders.go` adds CreateFolder / RenameFolder / DeleteFolder.
- [ ] action.Executor extends with the three folder action types,
      optimistic apply + rollback.
- [ ] Sidebar `N` / `R` / `X` wired in dispatchFolders.
- [ ] Name modal + confirm modal integrated.
- [ ] CLI `inkwell folder new|rename|delete` per spec 14 patterns.
- [ ] Tests:
      - graph: httptest fixtures for the three calls + the 403 well-known case.
      - action: optimistic apply rollback on failure.
      - dispatch: N/R/X open the right modals; commit dispatches the right action.
      - e2e: create-then-rename-then-delete a test folder; sidebar reflects each step.
- [ ] User docs: reference (`N`/`R`/`X` rows in folders pane,
      `:folder ...` commands), how-to ("Reorganise your mailbox"
      recipe).

## 9. Cross-cutting checklist

- [ ] Scopes: `Mail.ReadWrite` (already requested).
- [ ] Store reads/writes: folders.
- [ ] Graph endpoints: §4.
- [ ] Offline: the three actions queue and replay on reconnect
      via the existing engine drainer.
- [ ] Undo: rename and create are easily reversible (rename back,
      delete the new folder). Delete is server-soft (Deleted
      Items cascade); recovery is a Graph restore-from-deleted-
      items move, which we don't expose. Document in the
      explanation: "deletion is reversible from Outlook's Deleted
      Items folder."
- [ ] User errors: §7 table.
- [ ] Tests: §8 list.
