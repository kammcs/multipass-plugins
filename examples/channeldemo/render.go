//go:build wasip1

// The one slot this channel draws.
//
// A channel page is almost entirely host-filled: the hero, the two shelves
// and the heading are drawn from the libraries without this plugin being
// woken at all, which is what lets the owner reorder, retune and hide them
// with no risk of a slow or broken plugin taking the page down with it. A
// `slot` is the exception, and the only part of the page a plugin renders.
//
// So a slot should be the thing the host cannot know: an editor's note, a
// row assembled from somewhere else, a state line. Not a shelf of your own
// library, which the host already draws better and faster.
//
// The answer is cached under the slot's `cacheSeconds` (an hour here),
// keyed by plugin, slot and locale. The refs and tiles INSIDE it still
// resolve per viewer on every render, which is the phase 4 rule: the cache
// holds what the plugin said, never what a particular person is allowed to
// see.
package main

import "encoding/json"

// render answers a channel.render call. Every slot id this plugin serves
// is a case here, and anything else is an error rather than an empty
// answer: a slot that silently renders nothing looks to an owner exactly
// like a slot that is broken.
func render(raw json.RawMessage) response {
	var req renderRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable render request"}
		}
	}
	switch req.Slot {
	case "picks":
		return response{Data: picksSlot()}
	}
	return response{Error: "this channel has no slot called " + req.Slot}
}

// picksSlot is a row and a line: what the page around it is made of, and
// what happens to it once it belongs to the household.
//
// The tiles name no itemId and no picture, and that is a consequence worth
// reading. This plugin asks for NO capabilities, so it has never been told
// a local id for anything, including the titles its own sync created. A
// tile can only draw what a plugin already holds: letters, a symbol and a
// tint. To point a tile at a real title, ask for the library capability
// and look your own external ids up, the way examples/plugins/feeddemo
// does for its search rows.
//
// The last tile is the phase 4 rule made visible. It carries a title out
// of THIS PLUGIN'S catalog, and the shelves above it carry the same title
// out of the library. Rename it in the app and the shelves follow on the
// next render while this row does not, because what a plugin said is
// computed once and cached under the slot's cacheSeconds, and only the
// host's own cards are resolved for the person looking, every time. Which
// is exactly the split to design a slot around: put what the host cannot
// know in here, and let the host draw the titles.
func picksSlot() map[string]any {
	return map[string]any{
		"blocks": []any{
			map[string]any{
				"type":   "tiles",
				"layout": "row",
				"shape":  "wide",
				"text":   "What is on this page",
				"tiles": []any{
					map[string]any{
						"title":    "A hero and two shelves",
						"subtitle": "Drawn by the server",
						"badge":    "HOST",
						"art":      map[string]any{"glyph": "folder", "tint": "blue"},
					},
					map[string]any{
						"title":    "This row",
						"subtitle": "The only part the plugin draws",
						"badge":    "SLOT",
						// accent, not a colour of your own: the tints are a
						// closed set (gold, silver, bronze, accent, red,
						// green, blue, gray) and anything else is dropped, so
						// a tile asking for one draws on the plain panel and
						// looks broken beside its neighbours.
						"art": map[string]any{"glyph": "play", "tint": "accent"},
					},
					map[string]any{
						"title":    "Whatever you add next",
						"subtitle": "Shelves, headings, collections",
						"art":      map[string]any{"monogram": "MP", "tint": "gold"},
					},
					map[string]any{
						"title":    films[0].title,
						"subtitle": "The plugin's own word for it, cached",
						"art":      map[string]any{"monogram": "1", "tint": "silver"},
					},
				},
				// What an older client shows when it does not know the
				// vocabulary this block is written in. One sentence, so the
				// slot still says something rather than disappearing.
				"fallbackText": "The page around this row is drawn by the server; this row is the plugin's.",
			},
			map[string]any{
				"type": "notice",
				"tone": "info",
				"text": "This layout arrived with the plugin and belongs to your household now. Rearrange it, hide what you do not want, or add your own shelves; an update to the plugin adds new blocks at the bottom and leaves your arrangement alone.",
				"links": []any{
					map[string]any{
						"label":  "How channels work",
						"action": map[string]any{"kind": "url", "url": "https://multipass.theater/plugins/reference"},
					},
				},
				"fallbackText": "This layout arrived with the plugin and belongs to your household now.",
			},
		},
	}
}
