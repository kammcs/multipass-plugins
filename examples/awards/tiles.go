//go:build wasip1

// The presentation half: turning awards into tiles.
//
// This is the part worth reading if you are writing a plugin of your own
// (D174). The `tiles` block is slots, not a shape: art, two lines of text,
// a state mark, a link. The host knows nothing about awards, and nothing
// here describes a layout. What this file does is decide that the awarding
// BODY is the headline and the category is the second line, that a win
// gets a check and a nomination an open ring, and that a face goes in the
// circle when Wikidata named somebody and a monogram when it did not.
//
// A different plugin would make different choices out of the same slots
// and get a row that looks nothing like this one, which is the point.
package main

import (
	"strconv"
	"strings"
)

// body is how one awarding organisation is presented: what to call it,
// the letters for its disc, a tint, and a symbol.
type body struct {
	name  string
	mono  string
	tint  string
	glyph string
	major bool
}

// bodies is matched as a PREFIX of the award's full label, because
// Wikidata spells them out ("Academy Award for Best Cinematography"). The
// order matters: "Primetime Emmy" has to be tried before "Emmy".
//
// Symbols are the host's generic set, never a real logo. A ceremony's logo
// is a trademark and the Oscar statuette is AMPAS copyright, so what makes
// an Academy Award recognisable here is the monogram and the tint, not a
// picture we are not entitled to use.
var bodies = []struct {
	prefix string
	body
}{
	{"Academy Award", body{"Academy Award", "AA", "gold", "trophy", true}},
	{"BAFTA", body{"BAFTA", "BA", "bronze", "trophy", true}},
	{"Golden Globe", body{"Golden Globe", "GG", "gold", "trophy", true}},
	{"Primetime Emmy", body{"Emmy", "EM", "silver", "trophy", true}},
	{"Emmy", body{"Emmy", "EM", "silver", "trophy", true}},
	{"Screen Actors Guild", body{"SAG Award", "SAG", "silver", "trophy", true}},
	{"Directors Guild", body{"DGA Award", "DGA", "accent", "trophy", true}},
	{"Writers Guild", body{"WGA Award", "WGA", "accent", "trophy", true}},
	{"Palme d'Or", body{"Cannes", "CA", "green", "laurel", true}},
	{"Golden Lion", body{"Venice", "VE", "blue", "laurel", true}},
	{"Golden Bear", body{"Berlin", "BE", "gray", "laurel", true}},
}

// bodyOf works out who gave an award. A major is looked up; anything else
// is derived from the label itself, which is what makes the row still
// legible when the owner turns "major ceremonies only" off and forty
// regional critics' prizes arrive.
func bodyOf(label string) body {
	for _, b := range bodies {
		if strings.HasPrefix(label, b.prefix) {
			return b.body
		}
	}
	name := label
	if cut := strings.Index(label, " for "); cut > 0 {
		name = label[:cut]
	}
	name = strings.TrimSuffix(name, " Award")
	return body{name: name, mono: monogram(name), tint: "gray", glyph: "trophy"}
}

// category is the second line: what the award was actually for. An award
// with no category (a Palme d'Or is not "for" anything) says so by leaving
// the line empty rather than repeating its own name.
func category(label string) string {
	if cut := strings.Index(label, " for "); cut > 0 {
		return strings.TrimSpace(label[cut+len(" for "):])
	}
	for _, b := range bodies {
		if strings.HasPrefix(label, b.prefix) && b.prefix != b.body.name {
			return label // "Palme d'Or" under the heading "Cannes"
		}
	}
	return ""
}

// monogram makes letters for a disc: the initials of the first three
// words, which is what a reader recognises at cast-circle size.
func monogram(name string) string {
	var out []byte
	for _, w := range strings.Fields(name) {
		if len(out) >= 3 {
			break
		}
		c := w[0]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		if c >= 'A' && c <= 'Z' {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

// mark says what happened. The whole reason the row reads at a glance:
// the shape lands before any of the words do.
func mark(won bool) string {
	if won {
		return "check"
	}
	return "ring"
}

// tileFor builds one award's tile. The face is a request, not a promise:
// the plugin names a person by the id the host's own cast circles use and
// the HOST decides whether it has a picture, which is why the monogram and
// the tint are always set as well.
func tileFor(a award) map[string]any {
	b := bodyOf(a.Name)
	art := map[string]any{"monogram": b.mono, "tint": b.tint, "glyph": b.glyph}
	tile := map[string]any{"title": b.name, "mark": mark(a.Won), "art": art}
	if sub := category(a.Name); sub != "" {
		tile["subtitle"] = sub
	}
	if id := atoi64(a.WhoTMDB); id > 0 {
		art["personId"] = id
		tile["action"] = map[string]any{"kind": "person", "personId": id}
		// The person's name earns the third line nobody has, so it goes
		// where it is most use: an award with no category shows who won it
		// instead of an empty line.
		if _, has := tile["subtitle"]; !has && a.Who != "" {
			tile["subtitle"] = a.Who
		}
	}
	return tile
}

// atoi64 parses an id, returning 0 for anything that is not one. Written
// out rather than reaching for strconv.ParseInt so the failure mode is
// "no id" rather than an error nobody can act on.
func atoi64(s string) int64 {
	if s == "" || len(s) > 12 {
		return 0
	}
	var n int64
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
		n = n*10 + int64(s[i]-'0')
	}
	return n
}

// itoa and itoa64 keep the presentation code free of strconv noise; the
// plugin has exactly two numbers to print and both are ids or counts.
func itoa(n int) string { return strconv.Itoa(n) }

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// rowTiles is how many circles a detail page gets. A dozen fills the strip
// on every surface and the see-all tile carries the rest, which is the
// same bargain the app's own shelves make (D81).
const rowTiles = 12

// awardRow is the block a detail page gets: a row of circles, wins first,
// and a see-all when there was more than fits.
func awardRow(shown []award, itemID int64, wins, noms int) map[string]any {
	tiles := []any{}
	for i, a := range shown {
		if i >= rowTiles {
			break
		}
		tiles = append(tiles, tileFor(a))
	}
	block := map[string]any{
		"type":   "tiles",
		"layout": "row",
		"tiles":  tiles,
		// What an app built before tiles shows instead. Without this the
		// row is simply missing there, and the tally was the whole section
		// until now, so it is exactly the right thing to fall back to.
		"fallbackText": tally(wins, noms),
	}
	if len(shown) > len(tiles) {
		block["more"] = map[string]any{
			"label": "See all " + itoa(len(shown)),
			"action": map[string]any{
				"kind":   "page",
				"pageId": "title",
				"params": map[string]string{"id": itoa64(itemID)},
			},
		}
	}
	return block
}

// awardList is the same tiles stacked, for a page whose whole subject is
// the list. Same parts in a different order, deliberately: a page that
// lists what a row summarized should look like the row it came from.
func awardList(shown []award) map[string]any {
	tiles := []any{}
	for _, a := range shown {
		tiles = append(tiles, tileFor(a))
	}
	return map[string]any{"type": "tiles", "layout": "list", "tiles": tiles}
}
