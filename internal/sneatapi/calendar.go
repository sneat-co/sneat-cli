package sneatapi

import (
	"context"
	"net/http"

	"github.com/sneat-co/calendarius/backend/dto4calendarius"
)

// calendariusPath turns a calendarius route ("/v0/happenings/update_slot")
// into the relative path this Client's baseURL (already ending in ".../v0/")
// expects, the same way sneataiPath does for sneatai routes.
func calendariusPath(p string) string {
	return sneataiPath(p)
}

// UpdateSlot POSTs happenings/update_slot, replacing one slot's timing
// wholesale -- used for a non-recurring happening's reschedule (⛔ HTTP only,
// never a direct Firestore write).
func (c *Client) UpdateSlot(ctx context.Context, req dto4calendarius.HappeningSlotRequest) error {
	return c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/update_slot"), req, nil)
}

// AdjustSlot POSTs happenings/adjust_slot, a per-date deviation of a
// recurring happening's slot (e.g. "just this Friday moves to 16:00")
// rather than a wholesale rewrite.
func (c *Client) AdjustSlot(ctx context.Context, req dto4calendarius.HappeningSlotDateRequest) error {
	return c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/adjust_slot"), req, nil)
}

// CancelAdjustment POSTs happenings/cancel_adjustment, removing a per-date
// deviation adjust_slot staged -- the undo of AdjustSlot (B2 ruling),
// restoring that occurrence to the recurring template.
func (c *Client) CancelAdjustment(ctx context.Context, req dto4calendarius.HappeningDateSlotIDRequest) error {
	return c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/cancel_adjustment"), req, nil)
}

// CancelHappening POSTs happenings/cancel_happening.
func (c *Client) CancelHappening(ctx context.Context, req dto4calendarius.CancelHappeningRequest) error {
	return c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/cancel_happening"), req, nil)
}

// RevokeHappeningCancellation POSTs happenings/revoke_happening_cancellation
// -- the undo of CancelHappening. It takes the same CancelHappeningRequest
// shape (Date/SlotID included) CancelHappening does: calendarius's own
// facade4calendarius.RevokeHappeningCancellation needs the same Date/SlotID
// that cancelled an occurrence to un-cancel that same occurrence (S6) -- a
// prior version took the bare HappeningRequest, which could only ever
// revoke a whole-happening cancellation.
func (c *Client) RevokeHappeningCancellation(ctx context.Context, req dto4calendarius.CancelHappeningRequest) error {
	return c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/revoke_happening_cancellation"), req, nil)
}

// CreateHappening POSTs happenings/create_happening -- calendar.add_happening
// (founder ask, add-event-prompt.md scratchpad: "ability to add calendar
// events -- it's core must have feature"). Returns the created happening's
// ID so the caller (SneatExecutor.addHappening) can build an undo (permanent
// delete) and a resolvable EntityRef for the new happening.
func (c *Client) CreateHappening(ctx context.Context, req dto4calendarius.CreateHappeningRequest) (dto4calendarius.CreateHappeningResponse, error) {
	var out dto4calendarius.CreateHappeningResponse
	err := c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/create_happening"), req, &out)
	return out, err
}

// UpdateHappeningTexts POSTs happenings/update_happening_texts -- used for
// calendar.update_happening's rename-only slice (B: "Rename the sync to
// Weekly planning"). Every UpdateHappeningRequest field besides Title is a
// pointer left nil, so this call touches ONLY the title (see that DTO's own
// doc comment: an absent key is a no-op, not an erase).
func (c *Client) UpdateHappeningTexts(ctx context.Context, req dto4calendarius.UpdateHappeningRequest) error {
	return c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/update_happening_texts"), req, nil)
}

// DeleteHappening calls DELETE happenings/delete_happening -- m9's "re-add
// alongside a delete_happening intent if a later slice needs permanent
// deletion" now applies: this is addHappening's undo (a just-created
// happening has nothing to preserve, unlike CancelHappening's reversible
// mark), never something a decision or the main LLM asks for directly.
func (c *Client) DeleteHappening(ctx context.Context, req dto4calendarius.HappeningRequest) error {
	return c.do(ctx, http.MethodDelete, calendariusPath("/v0/happenings/delete_happening"), req, nil)
}
