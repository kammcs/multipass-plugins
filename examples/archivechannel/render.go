//go:build wasip1

// The two slots this channel draws.
//
// Almost none of this page is here. The hero, the eight shelves and the
// heading are drawn by the host straight out of the libraries, without
// this plugin being woken at all, which is what lets a household
// rearrange their channel with no risk of a slow or broken plugin taking
// the page down with it. A `slot` is the exception and the only part a
// plugin renders.
//
// So a slot should be what the host cannot know. Here that is the curated
// core (picks.go): a list somebody read and vouched for, which no sort
// over the archive's own index can reproduce, pointed at the real local
// titles the sync created.
//
// The answer is cached under the slot's cacheSeconds (an hour, per the
// manifest), keyed by plugin, slot and locale. What the plugin SAID is
// cached; the tiles inside it still resolve per viewer on every render,
// so the poster, the progress bar and the rating cap are always that
// person's own.
package main

import (
	"encoding/json"
	"strconv"
)

// maxRowTiles is the host's maxItemsInList. A tiles row is cut to it
// server-side, so counting to it here is only about not spending lookups
// on tiles that would be dropped.
const maxRowTiles = 24

// render answers a channel.render call. Every slot the manifest declares
// is a case here, and anything else is an ERROR rather than an empty
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
		return picksSlot()
	case "about":
		return aboutSlot()
	}
	return response{Error: "this channel has no slot called " + req.Slot}
}

// picksSlot is the curated row: a poster shelf of the titles this plugin
// vouches for, each tile opening the real local title.
//
// Returning an error when nothing resolved is deliberate and it is about
// the CACHE. A row the host could not fill is dropped either way, but an
// empty answer would be cached for the hour and an error is not, so the
// row comes back on the next render instead of on the next hour. The
// commonest reason for nothing to resolve is a first sync that has not
// finished yet, and making a household wait an hour past that would be a
// self-inflicted bug.
func picksSlot() response {
	tiles := []any{}
	for _, batch := range pickBatches(pickOrder()) {
		found, err := lookupItems(batch)
		if err != nil {
			return response{Error: err.Error()}
		}
		for _, p := range batch {
			id := found[p.ia]
			if id == 0 {
				// The host answers only about libraries the person asking
				// can see, so a miss here means this title is not in one
				// of them: not synced yet, in a library they cannot open,
				// or above their rating cap. A tile with nowhere to go is
				// not a tile, so it is left out rather than drawn dead.
				continue
			}
			tiles = append(tiles, map[string]any{
				"title":    p.title,
				"subtitle": pickYear(p.year),
				// The art names the item and NOTHING else. The host then
				// resolves this viewer's poster onto it, which for a
				// curated row is the real TMDB poster the whole table
				// exists to earn (D178 R2). A monogram or a glyph here
				// would be the fallback that gets drawn when the poster
				// is missing, and on a channel whose point is that it
				// looks like an app, letters on a tinted panel is the
				// thing to leave out rather than the thing to add.
				"art":    map[string]any{"itemId": id},
				"action": map[string]any{"kind": "item", "itemId": id},
			})
			if len(tiles) >= maxRowTiles {
				break
			}
		}
		if len(tiles) >= maxRowTiles {
			break
		}
	}
	if len(tiles) == 0 {
		return response{Error: "none of the curated titles are in a library this viewer can see yet"}
	}
	return response{Data: map[string]any{"blocks": []any{
		map[string]any{
			"type":   "tiles",
			"layout": "row",
			"shape":  "poster",
			"text":   "Start here",
			"tiles":  tiles,
			// What a client that does not know this block's vocabulary
			// shows instead, so the slot still says something rather than
			// disappearing off an older app.
			"fallbackText": "A shelf of films from the archive that somebody watched before putting them here.",
		},
	}}}
}

// pickOrder is the curated table in the order the row should draw it:
// the hero titles first, because those are the ones chosen to be the face
// of this channel, then the rest of the table in its own order.
func pickOrder() []pick {
	out := heroPicks()
	seen := map[string]bool{}
	for _, p := range out {
		seen[p.ia] = true
	}
	for _, p := range picks {
		if !seen[p.ia] {
			out = append(out, p)
		}
	}
	return out
}

// pickBatches cuts the curated list into calls the host will accept. The
// cap is maxLookupIDs (100) per call, and a plugin that sends more gets a
// refusal rather than a truncated answer.
func pickBatches(all []pick) [][]pick {
	out := [][]pick{}
	for i := 0; i < len(all); i += maxLookupIDs {
		end := i + maxLookupIDs
		if end > len(all) {
			end = len(all)
		}
		out = append(out, all[i:end])
	}
	return out
}

// lookupItems asks the host which of THIS plugin's external ids are items
// here, in this plugin's own namespace and only about libraries the
// viewer can see. It is the whole reason the manifest asks for the
// `library` capability, and it is the only thing that capability buys: no
// browsing, no reading anybody's library, just "is this id of mine a
// thing here, and what is its local number".
func lookupItems(batch []pick) (map[string]int64, error) {
	ids := make([]string, 0, len(batch))
	for _, p := range batch {
		ids = append(ids, p.ia)
	}
	req, err := json.Marshal(lookupOp{Provider: pluginID, IDs: ids})
	if err != nil {
		return nil, errString("could not ask the server about the curated titles")
	}
	var found lookupResult
	if err := json.Unmarshal(call(hostLookup, req), &found); err != nil {
		return nil, errString("the server's answer about the curated titles was unreadable")
	}
	if found.Error != "" {
		return nil, errString("the server could not look up the curated titles: " + found.Error)
	}
	out := map[string]int64{}
	for id, r := range found.Found {
		// ItemID only. A curated row is a film, and the table carries TMDB
		// MOVIE ids; a seriesId would need the `series` verb and a table
		// that vouches for shows, which this one does not.
		if r.ItemID > 0 {
			out[id] = r.ItemID
		}
	}
	return out, nil
}

// aboutSlot is the line at the bottom of the page telling a household the
// page is theirs. It is the one thing on a channel page that has to be
// said in words, because everything it describes is an affordance in the
// layout editor that nobody would go looking for.
func aboutSlot() response {
	return response{Data: map[string]any{"blocks": []any{
		map[string]any{
			"type": "notice",
			"tone": "info",
			"text": "This layout arrived with the plugin and belongs to your household now. " +
				"Rearrange the rows, hide the ones you do not want, or add your own shelves. " +
				"An update to this plugin adds new rows at the bottom and leaves your arrangement alone.",
			// One button, because a notice takes one: the second one makes
			// it a menu, and the host drops it rather than wrapping.
			"links": []any{
				map[string]any{
					"label":  "How channels work",
					"action": map[string]any{"kind": "url", "url": "https://multipass.theater/plugins/reference"},
				},
			},
			"fallbackText": "This layout arrived with the plugin and belongs to your household now.",
		},
	}}}
}

// yearText is the line under a tile's title. A curated row's year came off
// the table rather than off the archive's `year` field, which on a recent
// upload is the year somebody uploaded it, but it is bounded here anyway:
// this is the one place a bad year would be drawn to a viewer.
func pickYear(year int) string {
	if y := sensibleYear(year); y > 0 {
		return strconv.Itoa(y)
	}
	return ""
}
