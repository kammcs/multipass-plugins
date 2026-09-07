//go:build wasip1

// The gallery page: every block the vocabulary has, once each, in the
// order an author is most likely to want to read them.
//
// The order is deliberate. The hero first, because it is the only block
// that changes what the top of a page feels like. Then the two leaf
// widgets that replace what plugins used to fake out of fact lists and
// badge rows. Then the one container that holds blocks, so you can see
// what a split does to the two things inside it. Then the same `tiles`
// block four more times, once per shape, ending on the D174 circle so the
// old shape and the new ones sit on one screen and you can judge them
// against each other.
//
// Nothing here is conditional and nothing is clever. A showcase whose
// blocks appear only under some condition is a showcase you cannot trust
// when one is missing.
package main

// discArt is a tile drawn from letters and a colour: no picture, and none
// wanted. The host draws a monogram whenever the art resolves to nothing
// usable, so this is not a fallback, it is a choice.
func discArt(mono, tint string) map[string]any {
	return map[string]any{"monogram": mono, "tint": tint}
}

// glyphArt is a tile drawn from one of the closed set of symbols. Closed
// because each client draws its own idiom of the same name (an SVG path
// on web, an SF Symbol on Apple, a Material icon on Android), so a name
// nobody agreed on draws nothing at all.
func glyphArt(glyph, tint string) map[string]any {
	return map[string]any{"glyph": glyph, "tint": tint}
}

// galleryPage builds the whole thing. The subject arrives resolved, so
// the page can name a real title and point real links at it.
func galleryPage(s *subject) map[string]any {
	if s == nil || s.Item == nil {
		// A subject that has gone is not an error. Say so plainly rather
		// than returning nothing, which renders as a blank page.
		return map[string]any{
			"blocks": []any{
				map[string]any{"type": "text", "text": "This title is no longer in the library."},
			},
		}
	}
	it := s.Item
	return map[string]any{
		"title": "Every shape: " + it.Title,
		"blocks": []any{
			heroBlock(it),
			statsBlock(),
			columnsBlock(it),
			posterGrid(it),
			squareRow(),
			wideList(),
			circleGrid(),
			closingNotice(),
		},
	}
}

// heroBlock is the one layout that fills the width and pages through its
// tiles. It has no `shape`: a hero IS its own shape, and the sanitizer
// clears anything you set there, which is worth seeing rather than
// wondering about.
//
// rotateSeconds is 6. Zero turns rotation off; anything else is clamped
// to 4..60, because a hero that changes faster than you can read it is a
// bug in every review it has ever survived.
func heroBlock(it *item) map[string]any {
	return map[string]any{
		"type":          "tiles",
		"layout":        "hero",
		"rotateSeconds": 6,
		"tiles": []any{
			// The subject itself. Naming an `itemId` in `art` is how a
			// plugin gets the app's own picture for the person looking:
			// the host resolves a backdrop for a hero, per viewer, on
			// every render, and hands back nothing at all if this viewer's
			// profile is capped below the title's rating. A plugin cannot
			// leak a poster it was never shown.
			map[string]any{
				"title":    it.Title,
				"subtitle": "The title this page is about",
				// On a hero the badge is the EYEBROW above the title. On a
				// wide, poster or square card it is the chip on the art.
				// On a circle it is ignored: there is nowhere to put it.
				"badge": "SUBJECT",
				// `text` is hero only, up to 300 runes, and cleared on
				// every other layout. It is the description line a banner
				// has and a card has nowhere to put.
				"text": "A hero is one tile filling the width, with dots paging through the rest. The buttons below are the only place in the vocabulary a tile may carry buttons.",
				// A deliberate lie, and the host corrects it. Because this
				// tile's art names an itemId, the server overwrites
				// progress and watched with ITS truth for this viewer, so
				// a plugin can never draw a wrong watched tick on a title
				// the host knows about. Load the page and this bar shows
				// where you actually are, not 42%.
				"progress": 0.42,
				"watched":  true,
				"art":      map[string]any{"itemId": it.ID},
				// Buttons are hero only, at most two. Every other layout
				// drops them: a card with buttons is not a shape we draw
				// anywhere, and inventing one per plugin is how a row ends
				// up unfocusable on a TV.
				"links": []any{
					map[string]any{
						"label":  "Open the title",
						"action": map[string]any{"kind": "item", "itemId": it.ID},
					},
					map[string]any{
						"label": "Reload this page",
						"action": map[string]any{
							"kind":   "page",
							"pageId": "gallery",
							"params": map[string]string{"id": itoa64(it.ID)},
						},
					},
				},
			},
			// No picture, no id, no request of the host: letters and a
			// colour. This is what every tile falls back to, drawn on
			// purpose so you can see it is a real shape.
			map[string]any{
				"title":    "Letters and a tint",
				"subtitle": "No picture asked for",
				"text":     "When nothing in art resolves, the host draws a tinted panel with the title set large. Tints are named, never hex, so they follow the viewer's theme instead of fighting it.",
				"art":      discArt("MP", "gold"),
			},
			// One of the ten symbols the vocabulary grew. Ten more joined
			// the original eight for the categories plugins actually
			// build: books, games, podcasts, favourites, times, dates,
			// people, folders, links, playback.
			map[string]any{
				"title":    "A symbol instead",
				"subtitle": "One of eighteen",
				"text":     "Set a glyph when the thing has a category but no picture. A podcast, a game, a saved link: the symbol says what kind of thing it is before any of the words land.",
				"art":      glyphArt("podcast", "blue"),
			},
		},
		// An older client draws a hero as a plain row of circles cropped
		// from these pictures. Readable, not pretty, and the reason this
		// plugin declares a minHostVersion for the server half of the same
		// gap: an old SERVER drops the block entirely and says nothing.
		"fallbackText": "A rotating banner: the title, a tinted panel and a symbol tile.",
	}
}

// statsBlock is four figures worth reading at a glance. The block exists
// because plugins kept building this out of a fact list, where the number
// and its caption are the same size and neither one lands.
//
// A value is up to 16 runes and is drawn LARGE, so it wants to be a
// figure and a unit, not a sentence.
func statsBlock() map[string]any {
	return map[string]any{
		"type": "stats",
		"text": "What the vocabulary holds",
		"stats": []any{
			map[string]any{"value": "4", "label": "Tile layouts", "tint": "accent"},
			map[string]any{"value": "4", "label": "Tile shapes"},
			map[string]any{"value": "18", "label": "Glyphs", "tint": "blue"},
			// The point of the whole plugin, stated as a number.
			map[string]any{"value": "0", "label": "Permissions asked for", "tint": "green"},
		},
		"fallbackText": "4 layouts, 4 shapes, 18 glyphs, 0 permissions.",
	}
}

// columnsBlock is the only container that holds BLOCKS rather than ids,
// and the only addition an old client cannot degrade by itself: it would
// draw the two columns' contents in some order, and the order is the
// meaning. So the host flattens a columns block into its inner blocks, in
// column order, before it reaches a client that predates the block. That
// is what the vocabulary handshake is for.
//
// Depth is one. A columns block inside a column is dropped, deliberately:
// two levels of nesting is a layout language, and a layout language is a
// focus engine on four renderer families.
func columnsBlock(it *item) map[string]any {
	facts := []any{
		map[string]any{"label": "Kind", "value": it.Kind},
	}
	if it.Year > 0 {
		facts = append(facts, map[string]any{"label": "Year", "value": itoa(it.Year)})
	}
	if it.RuntimeMin > 0 {
		facts = append(facts, map[string]any{"label": "Runtime", "value": itoa(it.RuntimeMin) + " min"})
	}
	if it.Genres != "" {
		facts = append(facts, map[string]any{"label": "Genres", "value": it.Genres})
	}
	facts = append(facts, map[string]any{"label": "Split", "value": "wideLeft, so this column is twice the other"})

	return map[string]any{
		"type": "columns",
		// wideLeft is 2:1, wideRight is 1:2, equal is even. It only means
		// something for two columns; three are always even, because a
		// 2:1:1 grid is the start of a layout language.
		"split": "wideLeft",
		"columns": []any{
			// A column holds BLOCKS, plural: this one is a heading and a
			// fact list. The heading is its own block on purpose. A fact
			// list carries rows and nothing else, so a `text` on one is a
			// field no client draws, and a column that quietly lost its
			// title beside a column that kept one reads as a rendering bug.
			map[string]any{"blocks": []any{
				map[string]any{"type": "heading", "text": "The subject, as facts"},
				map[string]any{
					"type": "factList",
					"rows": facts,
				},
			}},
			map[string]any{"blocks": []any{barsBlock()}},
		},
		// Nearly unreachable, and set anyway: the host flattens this block
		// for any client that would need the line. If one ever arrives
		// holding the block intact, it says something true instead of
		// nothing.
		"fallbackText": "Facts about the title, beside a set of progress bars.",
	}
}

// barsBlock is the second widget: labelled bars, 0 to 1. Both ends are
// drawn on purpose here, because an empty bar and a full one are the two
// a renderer gets wrong and the two nobody puts in a screenshot.
func barsBlock() map[string]any {
	return map[string]any{
		"type": "progress",
		"text": "How far through",
		"bars": []any{
			map[string]any{"label": "Not started", "value": 0, "text": "0 of 10"},
			map[string]any{"label": "Part way", "value": 0.6, "text": "6 of 10"},
			map[string]any{"label": "Finished", "value": 1, "text": "10 of 10"},
		},
		"fallbackText": "Not started, 6 of 10, finished.",
	}
}

// posterGrid is the 2:3 card in a wrapping grid: the library's own shape,
// and the one to reach for when a plugin's page is a list of titles.
//
// A grid stays a wrapping grid on web-TV, unlike a wide grid, which is a
// row there by the existing rule for that class. Worth knowing before you
// pick a shape for a page a TV will open.
func posterGrid(it *item) map[string]any {
	return map[string]any{
		"type":   "tiles",
		"layout": "grid",
		"shape":  "poster",
		"text":   "Posters, wrapping",
		"tiles": []any{
			map[string]any{
				"title":    it.Title,
				"subtitle": "Poster resolved by the host",
				"art":      map[string]any{"itemId": it.ID},
			},
			// The refusal, drawn on purpose. `poster` and `backdrop` are
			// fields the HOST writes, per viewer, after it has checked
			// what that viewer is allowed to see. Anything a plugin puts
			// there is cleared before the block is stored, so these two
			// lines have no effect and this tile draws its monogram
			// instead. That is the rule that makes a tile safe: a plugin
			// supplies ids and asks, it never supplies resolved art and
			// tells.
			map[string]any{
				"title":    "Art the host refuses",
				"subtitle": "Cleared, so the card draws its own empty state",
				"art": map[string]any{
					"monogram": "NO",
					"tint":     "red",
					"poster":   "https://example.invalid/poster.jpg",
					"backdrop": "https://example.invalid/backdrop.jpg",
				},
			},
			map[string]any{
				"title":    "A mark",
				"subtitle": "check, ring, play, lock or none",
				"mark":     "play",
				"art":      discArt("PL", "accent"),
			},
			map[string]any{
				"title":    "Locked",
				"subtitle": "The mark says the state",
				"mark":     "lock",
				"art":      discArt("LK", "gray"),
			},
			map[string]any{
				"title":    "Part way",
				"subtitle": "The bar is card furniture",
				"progress": 0.35,
				"art":      discArt("35", "bronze"),
			},
			map[string]any{
				"title":    "Watched",
				"subtitle": "The tick, on a tile the host knows nothing about",
				"watched":  true,
				"art":      discArt("OK", "green"),
			},
		},
		"fallbackText": "A wrapping grid of poster cards.",
	}
}

// squareRow is 1:1 in a sideways strip: album art, podcast covers, and
// anything else whose source art was never 2:3 and looks cropped when you
// force it.
func squareRow() map[string]any {
	return map[string]any{
		"type":   "tiles",
		"layout": "row",
		"shape":  "square",
		"text":   "Squares, scrolling sideways",
		"tiles": []any{
			map[string]any{"title": "Soundtrack", "subtitle": "1:1 art", "art": glyphArt("music", "accent")},
			map[string]any{"title": "The podcast", "subtitle": "Episode 4", "art": glyphArt("podcast", "blue")},
			map[string]any{"title": "The book", "subtitle": "Adapted from", "art": glyphArt("book", "bronze")},
			map[string]any{"title": "The game", "subtitle": "Same universe", "art": glyphArt("game", "green")},
			map[string]any{"title": "Favourites", "subtitle": "Hearted", "art": glyphArt("heart", "red")},
			map[string]any{"title": "Coming up", "subtitle": "On the calendar", "art": glyphArt("calendar", "silver")},
		},
		// `more` is the trailing see-all, and it belongs on any row that had
		// to stop somewhere. It is drawn with the row's own card, so a
		// see-all on a square row is a square, and it takes a link like any
		// other: the host owns the verb, and a row that ends without one
		// just ends. The host puts one on a channel's capped shelf for the
		// same reason (D177 section 11a), so this is one see-all with one
		// look wherever it appears.
		"more": map[string]any{
			"label":  "Every shape, in the reference",
			"action": map[string]any{"kind": "url", "url": "https://multipass.theater/plugins/reference"},
		},
		"fallbackText": "A row of square cards.",
	}
}

// wideList is one tile per line: the thumbnail left, the words right. The
// shape to use when the second line matters as much as the picture, which
// is most of the time a plugin is listing something.
func wideList() map[string]any {
	return map[string]any{
		"type":   "tiles",
		"layout": "list",
		"shape":  "wide",
		"text":   "One per line",
		"tiles": []any{
			map[string]any{
				"title":    "A badge and a bar",
				"subtitle": "Both draw on the thumbnail",
				"badge":    "LIVE",
				"progress": 0.75,
				"art":      glyphArt("tv", "red"),
			},
			// The second refusal. `links` are hero only, at most two, and
			// dropped everywhere else: a card with buttons on it is not a
			// shape any of the seven surfaces draws, and a row of them is
			// how a TV page becomes impossible to move around with a
			// D-pad. This tile's buttons never reach a client. Its
			// `action` does, because a whole tile being one link is the
			// shape we do draw.
			map[string]any{
				"title":    "Buttons the host drops",
				"subtitle": "Use action for the whole tile instead",
				"badge":    "NOPE",
				"art":      glyphArt("link", "gray"),
				"links": []any{
					map[string]any{"label": "Play", "action": map[string]any{"kind": "url", "url": "https://multipass.theater"}},
					map[string]any{"label": "Details", "action": map[string]any{"kind": "url", "url": "https://multipass.theater"}},
				},
			},
			map[string]any{
				"title":    "Finished",
				"subtitle": "A full bar and a tick",
				"badge":    "1080p",
				"progress": 1,
				"watched":  true,
				"art":      glyphArt("film", "accent"),
			},
			map[string]any{
				"title":    "Waiting",
				"subtitle": "An empty bar is a real state",
				"badge":    "NEW",
				"progress": 0,
				"art":      glyphArt("clock", "silver"),
			},
		},
		"fallbackText": "A list of wide cards with badges and bars.",
	}
}

// circleGrid is the original tile, unchanged since D174: the cast circle.
// It is last so the shape everything else grew out of sits on the same
// screen as the four that came after, which is the only honest way to
// judge whether a new shape earned its place.
func circleGrid() map[string]any {
	return map[string]any{
		"type":   "tiles",
		"layout": "grid",
		"shape":  "circle",
		"text":   "Circles, the original tile",
		"tiles": []any{
			map[string]any{"title": "Academy Award", "subtitle": "Best Picture", "mark": "check", "art": discArt("AA", "gold")},
			map[string]any{"title": "BAFTA", "subtitle": "Nominated", "mark": "ring", "art": discArt("BA", "bronze")},
			map[string]any{"title": "Cannes", "subtitle": "Palme d'Or", "mark": "check", "art": glyphArt("laurel", "green")},
			map[string]any{"title": "A person", "subtitle": "Drawn like a cast circle", "art": glyphArt("person", "silver")},
			map[string]any{"title": "A folder", "subtitle": "Somewhere to put things", "art": glyphArt("folder", "blue")},
			map[string]any{"title": "Start", "subtitle": "The play mark", "mark": "play", "art": glyphArt("play", "accent")},
			// A verb that does not exist, on purpose, and the third refusal
			// on this page. The action set is CLOSED (url, item, person,
			// series, library, page): the host drops anything else before
			// the block is stored, and a client that met one anyway draws
			// the tile UNPRESSABLE rather than as a card that takes a click
			// and does nothing. That second half is the rule an older app
			// needs the day a newer server learns a verb, so it is drawn
			// here where you can try to press it.
			map[string]any{
				"title": "A verb from a newer server", "subtitle": "Drawn, never pressable",
				"art":    discArt("??", "gray"),
				"action": map[string]any{"kind": "playlist", "playlistId": 7},
			},
		},
		// A badge is ignored on a circle, so none of these set one. There
		// is nowhere on a disc to put a chip without covering the face.
		"fallbackText": "A wrapping grid of circles, the D174 cast-row tile.",
	}
}

// closingNotice is the same widget the detail page used, in the other
// register: a warning with nothing to press. A notice takes at most ONE
// button, because a notice with a menu is a links block wearing a colour.
func closingNotice() map[string]any {
	return map[string]any{
		"type": "notice",
		"tone": "warning",
		"text": "Three tiles on this page ask for things the host refuses: resolved art in the poster grid, buttons in the wide list, a verb the action set does not have in the circle grid. All three are cleared before the page is stored, and the last one is drawn without a hit target rather than as a card that takes a click and does nothing. Read gallery.go to see where.",
		// No links. A tone and a sentence is a complete notice.
		"fallbackText": "Three tiles on this page ask for things the host refuses. Read gallery.go.",
	}
}
