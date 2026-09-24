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

// CancelHappening POSTs happenings/cancel_happening.
func (c *Client) CancelHappening(ctx context.Context, req dto4calendarius.CancelHappeningRequest) error {
	return c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/cancel_happening"), req, nil)
}

// RevokeHappeningCancellation POSTs happenings/revoke_happening_cancellation
// -- the undo of CancelHappening.
func (c *Client) RevokeHappeningCancellation(ctx context.Context, req dto4calendarius.HappeningRequest) error {
	return c.do(ctx, http.MethodPost, calendariusPath("/v0/happenings/revoke_happening_cancellation"), req, nil)
}

// DeleteHappening calls DELETE happenings/delete_happening.
func (c *Client) DeleteHappening(ctx context.Context, req dto4calendarius.HappeningRequest) error {
	return c.do(ctx, http.MethodDelete, calendariusPath("/v0/happenings/delete_happening"), req, nil)
}
