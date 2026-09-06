//go:build wasip1

// The Wikidata half: the two queries, the cache in front of them, and the
// tidying every free data source needs.
//
// Split out of main.go when the tile row arrived (D174), because the file
// was carrying three jobs at once and this is the one an author reading
// the example actually wants to copy: identify a title by an id the host
// already has, ask a public source about it, and cache the answer.
package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

const endpoint = "https://query.wikidata.org/sparql"

// cacheDays is how long an answer is trusted. A year-old film gains no new
// awards; a recent one gains them in bursts around ceremony season, and a
// week late is nobody's emergency.
const cacheDays = 7

// award is one row of somebody's or something's honours.
type award struct {
	Name string `json:"name"`
	Year int    `json:"year,omitempty"`
	Won  bool   `json:"won"`
	// Who and WhoTMDB are the person the award was given to, when Wikidata
	// records one (D174). A win names its winner most of the time; a
	// nomination names its nominee much less often, which is why the tile
	// row can never be faces alone. WhoTMDB is the SAME id-space the host's
	// own cast circles use, so naming it is all the host needs to find the
	// headshot it already has.
	Who      string `json:"who,omitempty"`
	WhoTMDB  string `json:"whoTmdb,omitempty"`
	// Work and WorkTMDB are set only on a PERSON's awards: which film the
	// award was for, and its TMDB id if Wikidata knows one. That id is what
	// the library lookup runs on.
	Work     string `json:"work,omitempty"`
	WorkTMDB string `json:"workTmdb,omitempty"`
}

// sparql runs one query and returns its rows, each column flattened to its
// value string. An empty result and a failure are told apart by the error.
func sparql(query string) ([]map[string]string, error) {
	cfg := settings()
	// Wikidata asks callers to identify themselves and throttles the ones
	// that do not. The host sets a generic User-Agent when we send none;
	// naming the owner's contact address is the difference between a polite
	// client and an anonymous one.
	agent := "Multipass-Awards/1.1"
	if c := strings.TrimSpace(cfg["contact"]); c != "" {
		agent += " (" + c + ")"
	}
	raw := send(hostHTTP, map[string]any{
		"method": "GET",
		"url":    endpoint + "?format=json&query=" + escape(query),
		"headers": map[string]string{
			"Accept":     "application/sparql-results+json",
			"User-Agent": agent,
		},
	})
	var resp struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, errString("the query service gave an unreadable reply")
	}
	if resp.Error != "" {
		return nil, errString(resp.Error)
	}
	if resp.Status == 429 {
		return nil, errString("Wikidata is rate limiting us; try again in a minute")
	}
	if resp.Status != 200 {
		return nil, errString("Wikidata answered HTTP " + strconv.Itoa(resp.Status))
	}
	var body struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(resp.Body), &body); err != nil {
		return nil, errString("could not read the query result")
	}
	rows := make([]map[string]string, 0, len(body.Results.Bindings))
	for _, b := range body.Results.Bindings {
		row := map[string]string{}
		for k, v := range b {
			row[k] = v.Value
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// escape percent-encodes a query for the URL. Written out rather than
// imported because net/url drags a lot of the standard library into the
// module, and a plugin's whole binary is downloaded by every server.
func escape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0xF])
	}
	return b.String()
}

type errString string

func (e errString) Error() string { return string(e) }

// titleQuery asks what a film or series won and was nominated for, and WHO
// it went to. Both halves are one query because two round trips for one
// page is rude.
//
// P1346 is the winner qualifier and P2453 the nominee one; P4985 on that
// person is their TMDB id. Reaching for the person here is what turns a
// row of anonymous discs into a row of faces (D174), and it costs nothing:
// the qualifier is already on the statement we are reading.
const titleQuery = `SELECT ?kind ?awardLabel ?year ?whoLabel ?tmdb WHERE {
 { ?w wdt:P4947 "%ID%" } UNION { ?w wdt:P4983 "%ID%" }
 { ?w p:P166 ?st. ?st ps:P166 ?award. BIND("won" AS ?kind) }
 UNION
 { ?w p:P1411 ?st. ?st ps:P1411 ?award. BIND("nom" AS ?kind) }
 OPTIONAL { ?st pq:P585 ?when. BIND(YEAR(?when) AS ?year) }
 OPTIONAL { { ?st pq:P1346 ?who } UNION { ?st pq:P2453 ?who }
            OPTIONAL { ?who wdt:P4985 ?tmdb } }
 SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
} LIMIT 300`

// personQuery asks what a person won, and for which work. P4985 is the
// TMDB person id, which is the same id-space the host's person pages use,
// so no extra lookup is needed to identify them.
const personQuery = `SELECT ?awardLabel ?year ?workLabel ?tmdb WHERE {
 ?p wdt:P4985 "%ID%".
 ?p p:P166 ?st. ?st ps:P166 ?award.
 OPTIONAL { ?st pq:P585 ?when. BIND(YEAR(?when) AS ?year) }
 OPTIONAL { ?st pq:P1686 ?work. OPTIONAL { ?work wdt:P4947 ?tmdb } }
 SERVICE wikibase:label { bd:serviceParam wikibase:language "en". }
} LIMIT 200`

// cacheVersion is bumped when the SHAPE of a cached answer changes. The
// stored blob is our own JSON, so an old entry unmarshals cleanly into the
// new struct and silently lacks the new fields: a row of faces would come
// back blank for a week with nothing to show why. Changing the key is one
// character and makes the old entries unreachable instead.
const cacheVersion = "2"

// fetch runs one of the two queries, through the cache. The cache stores a
// stamp so an entry can age out; storage has no TTL of its own.
func fetch(kind, id string) ([]award, error) {
	key := cacheVersion + ":" + kind + ":" + id
	if raw := cacheGet(key); raw != "" {
		var c struct {
			At     int64   `json:"at"`
			Awards []award `json:"awards"`
		}
		if json.Unmarshal([]byte(raw), &c) == nil &&
			time.Since(time.Unix(c.At, 0)) < cacheDays*24*time.Hour {
			return c.Awards, nil
		}
	}
	q := titleQuery
	if kind == "person" {
		q = personQuery
	}
	rows, err := sparql(strings.ReplaceAll(q, "%ID%", id))
	if err != nil {
		return nil, err
	}
	var out []award
	for _, r := range rows {
		name := strings.TrimSpace(r["awardLabel"])
		if name == "" || strings.HasPrefix(name, "Q") && isQID(name) {
			continue // an unlabelled entity is noise, not an award
		}
		a := award{
			Name: name, Won: r["kind"] != "nom",
			Work: r["workLabel"], WorkTMDB: r["tmdb"],
		}
		if kind != "person" {
			// On a title's awards the tmdb column is the PERSON's; on a
			// person's it is the work's. Same column name, different
			// subject, so it is unpacked per query rather than guessed.
			a.Who, a.WhoTMDB, a.Work, a.WorkTMDB = r["whoLabel"], r["tmdb"], "", ""
			if isQID(a.Who) {
				a.Who = ""
			}
		}
		if y, err := strconv.Atoi(r["year"]); err == nil {
			a.Year = y
		}
		out = append(out, a)
	}
	out = collapse(out)
	if blob, err := json.Marshal(map[string]any{"at": time.Now().Unix(), "awards": out}); err == nil {
		cacheSet(key, string(blob))
	}
	return out, nil
}

// isQID spots a bare entity id, which is what a label lookup returns when
// the entity has no English label.
func isQID(s string) bool {
	if len(s) < 2 || s[0] != 'Q' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// collapse folds a nomination into the win it became, and several people
// into the one award they shared.
//
// Wikidata records all of it separately, and correctly: a film that WON
// Best Sound was also nominated for it, and a screenplay award won by two
// writers is two statements. Listing either twice reads as a mistake, so
// the win wins and the first named person keeps the circle. Which person
// that is comes out of Wikidata's order rather than any judgement of ours,
// which is honest: the award went to both.
func collapse(in []award) []award {
	won := map[string]bool{}
	for _, a := range in {
		if a.Won {
			won[a.Name+"|"+strconv.Itoa(a.Year)] = true
		}
	}
	seen := map[string]int{}
	out := in[:0]
	for _, a := range in {
		if !a.Won && won[a.Name+"|"+strconv.Itoa(a.Year)] {
			continue
		}
		key := a.Name + "|" + strconv.Itoa(a.Year) + "|" + strconv.FormatBool(a.Won)
		if at, dup := seen[key]; dup {
			// The same award, already kept. Only fill in a person if the
			// one we kept had none.
			if out[at].WhoTMDB == "" && a.WhoTMDB != "" {
				out[at].Who, out[at].WhoTMDB = a.Who, a.WhoTMDB
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Won != out[j].Won {
			return out[i].Won // wins first
		}
		if out[i].Year != out[j].Year {
			return out[i].Year > out[j].Year
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// keep applies the owner's "major ceremonies only" choice.
func keep(in []award) []award {
	if settings()["majorOnly"] == "false" {
		return in
	}
	var out []award
	for _, a := range in {
		if bodyOf(a.Name).major {
			out = append(out, a)
		}
	}
	return out
}

func counts(in []award) (wins, noms int) {
	for _, a := range in {
		if a.Won {
			wins++
		} else {
			noms++
		}
	}
	return
}

// tally is the one-line summary. It is no longer drawn on a detail page
// (the row says it better), but it is what an app too old to know the tile
// block falls back to, so it still has to be right.
func tally(wins, noms int) string {
	switch {
	case wins > 0 && noms > 0:
		return plural(wins, "win", "wins") + ", " + plural(noms, "nomination", "nominations")
	case wins > 0:
		return plural(wins, "win", "wins")
	case noms > 0:
		return plural(noms, "nomination", "nominations")
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
