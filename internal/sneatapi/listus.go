package sneatapi

import (
	"context"
	"net/http"

	"github.com/sneat-co/listus/backend/dto4listus"
)

func listusPath(p string) string {
	return sneataiPath(p)
}

// CreateListItems POSTs listus/list_items_create -- used for todo/to-buy
// item creation (⛔ HTTP only, never a direct Firestore write).
func (c *Client) CreateListItems(ctx context.Context, req dto4listus.CreateListItemsRequest) (dto4listus.CreateListItemResponse, error) {
	var out dto4listus.CreateListItemResponse
	err := c.do(ctx, http.MethodPost, listusPath("/v0/listus/list_items_create"), req, &out)
	return out, err
}

// SetListItemsIsDone POSTs listus/list_items_set_is_done -- used for todo
// complete/reopen (IsDone true/false is the same endpoint both ways, which
// is also how Undo of a completion is implemented: call it again with
// IsDone flipped).
func (c *Client) SetListItemsIsDone(ctx context.Context, req dto4listus.ListItemsSetIsDoneRequest) error {
	return c.do(ctx, http.MethodPost, listusPath("/v0/listus/list_items_set_is_done"), req, nil)
}

// DeleteListItems calls DELETE listus/list_items_delete.
func (c *Client) DeleteListItems(ctx context.Context, req dto4listus.ListItemIDsRequest) error {
	return c.do(ctx, http.MethodDelete, listusPath("/v0/listus/list_items_delete"), req, nil)
}
