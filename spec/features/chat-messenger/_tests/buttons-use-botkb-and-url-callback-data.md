---
format: https://specscore.md/scenario-specification
---

# Scenario: inline buttons use botkb types and URL-shaped callback data

**Validates:** [chat-messenger#ac:processing-is-swappable](../README.md#ac-processing-is-swappable), [chat-messenger#req:botkb-vocabulary](../README.md#req-botkb-vocabulary), [chat-messenger#req:callback-data-url](../README.md#req-callback-data-url)

## Steps

GIVEN a local processor backed by a fake spaces reader returning a space with id `family1`
WHEN `SendText` is called with `/spaces`
THEN the returned reply's `Keyboard` is a `botkb.Keyboard` of type `botkb.KeyboardTypeInline`
AND its buttons are `botkb.DataButton` values arranged in rows (`[][]botkb.Button`)
AND the button for `family1` carries callback data `space?id=family1`

GIVEN the callback data `space?id=family1`
WHEN it is parsed with `url.Parse`
THEN the URL path is `space`
AND the query argument `id` is `family1`

## TODO

- [ ] Implement as a unit test over the local processor.
- [ ] Assert the callback data parses under the same `url.Parse` contract `bots-fw`'s router applies, so the format stays compatible with a future server-side processor.

---
*This document follows the https://specscore.md/scenario-specification*
