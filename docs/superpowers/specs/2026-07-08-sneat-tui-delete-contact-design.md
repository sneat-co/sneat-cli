# Sneat TUI — Delete Contact Design

**Goal:** Let a user delete a contact from the interactive TUI, with a required
confirmation step and a guard that prevents deleting themselves.

## Requirements

- Delete a contact from the TUI.
- Cannot delete *yourself* — the contact whose `UserID` equals the signed-in uid.
- A confirmation is required before the (real, production) delete fires.

## Self-identification

`dbo4contactus.ContactDbo` embeds `WithUserID` (via `ContactBase → ContactBrief`),
exposing `GetUserID()`. A contact is "self" when `d.GetUserID() == uid`, where `uid`
is the value already threaded into the TUI (`sess.UID`). No extra lookup needed.

## Key bindings

Alphanumeric keys are reserved for the list's built-in filter, so delete is bound
to the non-alphanumeric **`DEL`** and **`BACKSPACE`** keys. `q` no longer quits
(the list's quit keybinding is disabled); quitting is `ctrl+c`, or `esc` at the
root Spaces screen. Filtering is the built-in bubbles/list behaviour, triggered
by `/`, and the footer hints advertise it.

## Trigger points

`DEL`/`BACKSPACE` delete in **both** places (routing to the same confirmation
screen), but only when the list is not filtering (while filtering, `BACKSPACE`
edits the filter text):

- On the highlighted row in the **Contacts / Members list**.
- On an open **contact card**.

Pressing delete on a self contact does not open the confirm screen; a transient
"Cannot delete yourself" flash is shown instead.

## Confirmation

Deleting a non-self contact pushes an in-TUI `confirmDeleteScreen`
(stays inside bubbletea, fully unit-testable):

```
Delete contact

Delete "<name>"?
This cannot be undone.

enter/del confirm · esc cancel
```

- `enter` / `del` / `backspace` issue the delete against the sneat-go API
  (`ContactDeleter.DeleteContact`).
- `esc` / `left` cancel (pop back).
- While deleting, keys are ignored and "Deleting…" is shown.
- On error: the error is shown inline and the screen stays open.
- On success: the stack unwinds to the Contacts list, that space's contact
  cache is invalidated, and the list reloads so the row disappears.

## Wiring

- New `tui.ContactDeleter` interface: `DeleteContact(ctx, spaceID, contactID string) error`.
  Threaded through `Run` → `Model`.
- `commands.ContactDeleter` (same signature) plus a `contactDeleter` adapter over
  the existing `ContactWriter.DeleteContact(ctx, dto4contactus.ContactRequest)`.
- `Env.RunTUI` gains the deleter parameter; `runSpaceUI` builds it from
  `NewContactWriter`; `main.go` wires `tui.Run`.
- When the deleter is nil (defensive; not a real runtime path), `d` is a no-op.

## Testing

- `contactItemsFrom` marks `isSelf` from uid.
- Confirm screen: `enter`/`del` call the deleter; `esc` cancels; delete error renders inline.
- Self contact: delete does not push a confirm screen (flash instead).
- `q` does not quit; `ctrl+c` does.
- Successful delete unwinds to the list, invalidates cache, and reloads.
- Command layer: `runSpaceUI` passes a non-nil deleter; adapter maps to the DTO request.
