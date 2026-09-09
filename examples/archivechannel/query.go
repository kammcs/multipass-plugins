//go:build wasip1

// The eight libraries and the one query builder they all go through.
//
// The owner never types a collection slug here, which is the whole point:
// examples/plugins/archive is the plugin where an owner picks a collection
// and gets whatever that collection is. This one ships the eight it has
// counted, so the query is derived from the library key the host sends.
//
// Two things in here are load-bearing:
//
//   - `format:"h.264"` is ANDed into every query. That is the archive's own
//     MP4 derivative, and asking the index for it is the difference between
//     a shelf of films and a shelf of rows that fail at Play. The proof is
//     in docs/ARCHIVE-CHANNEL.md section 2: `cliffhangers` holds 59 movie
//     serials and exactly 1 playable copy, and `classic_cartoons` holds 81
//     and none. Counted without the filter both look like good libraries.
//   - the floors are a DEFAULT, not a fallback. A config value this plugin
//     does not recognise means `reviewed`, never `everything`: the wide
//     shelf on a family server has to be chosen.
//   - every floor constrains avg_rating, not just num_reviews, and that is
//     the correction a measurement forced. The comedy collection's top row
//     by downloads is `sex_madness`, a 1938 exploitation film the archive
//     has filed under Comedy_Films, and it carries 43 reviews and 3.7
//     million downloads. No review count excludes it. Its rating is 2.91,
//     so a rating floor does, and every library got one.
package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// libDef is one of the eight, as the manifest declares it. `query` is the
// collection clause and nothing else: the media type and the format filter
// are added by searchURL so no library can be written without them.
//
// `floor` is the extra clause the `depth` setting adds when it is set to
// `reviewed`. Every library has one: the archive publishes no content
// rating, so the only signal about a title is what its own viewers said
// about it, and the tail below that signal is where the trouble is.
type libDef struct {
	key   string
	name  string
	shape string // "movie" or "show"
	query string
	floor string
}

// libs is the eight, keys matching manifest.json exactly. The playable
// counts beside them were measured on 2026-09-08 with the format filter on
// and will drift; they are here as the order of magnitude that justifies
// each library existing, not as a number anything checks.
var libs = []libDef{
	{
		key:   "films",
		name:  "Feature Films",
		shape: "movie",
		query: `collection:"feature_films"`,
		// 16,094 playable and no content classification of any kind, so
		// the default takes the 445 the archive's own viewers reviewed and
		// rated well. Sorting instead of filtering does not work here:
		// `downloads desc` on this collection opens with `sex_madness`.
		//
		// The floor is heavier than its neighbours' for a second reason:
		// it brings the result under the 2,000 this plugin will page
		// through, so the pass actually REACHES the end and may report
		// `complete`. A library whose every pass is truncated can only
		// ever grow.
		floor: `num_reviews:[5 TO *] AND avg_rating:[3.8 TO 5]`,
	},
	{
		key:   "noir",
		name:  "Film Noir",
		shape: "movie",
		query: `collection:"Film_Noir"`,
		// 738 playable to 203.
		floor: `num_reviews:[1 TO *] AND avg_rating:[3.5 TO 5]`,
	},
	{
		key:   "scifi",
		name:  "Science Fiction and Horror",
		shape: "movie",
		// Two collections because the archive split this one and neither
		// half is a library on its own. Parenthesised because AND binds
		// tighter than OR, so without the brackets the format filter would
		// apply to only one of them.
		query: `(collection:"SciFi_Horror" OR collection:"film_scifi")`,
		// 825 playable to 287.
		floor: `num_reviews:[1 TO *] AND avg_rating:[3.5 TO 5]`,
	},
	{
		key:   "comedy",
		name:  "Comedy",
		shape: "movie",
		query: `collection:"Comedy_Films"`,
		// 1,042 playable to 344, and this is the library the floor was found by: see the file comment.
		floor: `num_reviews:[1 TO *] AND avg_rating:[3.5 TO 5]`,
	},
	{
		key:   "silent",
		name:  "The Silent Era",
		shape: "movie",
		query: `collection:"silent_films"`,
		// 2,592 playable to 183. The drop is steep because almost nothing here has ever been reviewed, and an unwatched silent print is usually unwatchable.
		floor: `num_reviews:[1 TO *] AND avg_rating:[3.5 TO 5]`,
	},
	{
		key:   "cartoons",
		name:  "Animation",
		shape: "movie",
		query: `(collection:"more_animation" OR collection:"vintage_cartoons")`,
		// 599 playable to 128.
		floor: `num_reviews:[1 TO *] AND avg_rating:[3.5 TO 5]`,
	},
	{
		key:   "newsreel",
		name:  "Newsreels and Ephemera",
		shape: "movie",
		query: `(collection:"prelinger" OR collection:"universal_newsreels" OR collection:"avgeeks")`,
		// 11,086 playable, much of it raw footage nobody has ever watched.
		// Two reviews and a rating cuts it to 2,311, which still overruns
		// the 2,000 this plugin pages through: newsreels are a bottomless
		// well and this library only ever grows. That is the honest shape
		// for it, not a bug to tune away.
		floor: `num_reviews:[2 TO *] AND avg_rating:[3.5 TO 5]`,
	},
	{
		key:   "tv",
		name:  "Classic Television",
		shape: "show",
		query: `collection:"classic_tv"`,
		// 8,057 playable to 2,021. The floor earns its place twice here:
		// this is the library that spends one metadata call per title, so
		// cutting three quarters of the candidates cuts three quarters of
		// the round trips.
		floor: `num_reviews:[1 TO *] AND avg_rating:[3.5 TO 5]`,
	},
}

func libByKey(key string) (libDef, bool) {
	for _, l := range libs {
		if l.key == key {
			return l, true
		}
	}
	return libDef{}, false
}

// rows is one page of the search index and also one page of the answer:
// the host caps a sync page at 200 rows, so 100 leaves room for the
// curated rows page one prepends.
const rows = 100

// showRows is the same thing for the show-shaped library, and it is a
// fifth of the size because a page there is not one request, it is one
// search plus one metadata call PER ROW. Measured against the live service
// on 2026-09-08 a metadata call is about 0.28s, so a hundred-row page is
// half a minute of round trips before any parsing, and the first attempt
// at it died on the plugin's own 20-second call deadline. Twenty rows is
// about six seconds of network.
//
// A page is a unit of work, not a unit of counting, and the unit of work
// here is much more expensive. That is the whole reason this constant
// exists rather than a bigger timeout.
const showRows = 20

// The three values of the per-library `depth` setting, and the ladder they
// make. Anything unrecognised, including nothing at all, means `curated`.
//
// `curated` is the DEFAULT and it means this plugin's own table and nothing
// else, which is a decision the build made rather than the plan. Reading
// what the queries actually return is what forced it. Under a rating floor,
// on a family server's front page, the tail still holds `Diary of a Nudist`
// and `Child Bride` in features, an early-erotica cluster in silent film,
// `Nekromantik` in horror, cartoons the studios themselves withdrew for
// racist caricature, and uploads that are plainly still in copyright
// (`Double. Indemnity. 1944.720p. Br Rip.x265` is a real row). Section 3
// ruled that curation here has to be an allowlist because the archive
// publishes nothing to filter on. This is that ruling followed all the way
// down: the allowlist is not the top of the shelf, it IS the shelf, and the
// catalog behind it is something an owner turns on knowing what it is.
const (
	curatedOnly = "curated"
	reviewed    = "reviewed"
	deep        = "everything"
)

// catalogDepth reads the setting. A value this plugin does not recognise
// means the narrowest option, never the widest: on a family server the wide
// shelf has to be chosen, and a typo is not a choice.
func catalogDepth(cfg map[string]string) string {
	switch strings.TrimSpace(cfg["depth"]) {
	case reviewed:
		return reviewed
	case deep:
		return deep
	}
	return curatedOnly
}

// searchFields is what the index is asked to return. Asking for fewer
// fields than the index holds is the difference between a page of JSON and
// a megabyte of it, and every one of these is read by item().
var searchFields = []string{"identifier", "title", "year", "description", "runtime"}

// searchURL builds one page of one library's enumeration, at the movie
// page size. A show library asks for its own through searchURLRows.
func searchURL(l libDef, cfg map[string]string, page int) string {
	return searchURLRows(l, cfg, page, rows)
}

// searchURLRows is searchURL with the page size named, because the show
// library's page costs a round trip per row and cannot be the same size.
func searchURLRows(l libDef, cfg map[string]string, page, n int) string {
	q := l.query
	if l.floor != "" && catalogDepth(cfg) != deep {
		q += " AND " + l.floor
	}
	// mediatype excludes the collection's texts and audio; format is the
	// clause the whole plugin stands on. Neither is optional, which is why
	// neither is written in a libDef.
	q += ` AND mediatype:movies AND format:"h.264"`

	var b strings.Builder
	b.WriteString("https://archive.org/advancedsearch.php?q=")
	b.WriteString(pathEscape(q))
	b.WriteString("&rows=" + strconv.Itoa(n))
	b.WriteString("&page=" + strconv.Itoa(page))
	b.WriteString("&output=json")
	for _, f := range searchFields {
		b.WriteString("&" + pathEscape("fl[]") + "=" + pathEscape(f))
	}
	// Most-downloaded first, so a collection truncated at twenty pages is
	// truncated at the end nobody misses. The identifier breaks ties, which
	// is what keeps paging stable across the requests of one pass.
	b.WriteString("&" + pathEscape("sort[]") + "=" + pathEscape("downloads desc"))
	b.WriteString("&" + pathEscape("sort[]") + "=" + pathEscape("identifier asc"))
	return b.String()
}

// ---- reading a search index that answers in whatever shape it likes ----

// loose is a field the index returns as a string, as a number, or as a
// list of either, depending on the item: `year` comes back as 1938 on one
// row and "1938" on the next, and `description` is regularly a list of
// paragraphs. This type is copied from examples/plugins/archive rather
// than reinvented because a plugin that assumes one shape drops rows for
// no reason a person could guess, and a stub cannot reproduce it.
type loose struct{ s string }

func (l *loose) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil // a field we cannot read is a field we do without
	}
	l.s = flatten(v)
	return nil
}

func flatten(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := []string{}
		for _, e := range t {
			if s := flatten(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

type searchDoc struct {
	Identifier  string `json:"identifier"`
	Title       loose  `json:"title"`
	Year        loose  `json:"year"`
	Description loose  `json:"description"`
	Runtime     loose  `json:"runtime"`
}

type searchResult struct {
	Response struct {
		NumFound int         `json:"numFound"`
		Start    int         `json:"start"`
		Docs     []searchDoc `json:"docs"`
	} `json:"response"`
}
