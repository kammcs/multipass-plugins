//go:build wasip1

// Classic Television: the one library where an archive item is not a title.
//
// The other seven are movie-shaped and never touch the metadata endpoint at
// sync: one search page is one page of films. This one cannot work that way.
// Measured against the forty most-downloaded `classic_tv` items, 2026-09-08:
//
//	GreenAcresCompleteSeries   169 files, six season folders, numbered 001-170
//	get-smart                  137, "Get Smart S01E01 (Mr. Big).ia.mp4"
//	Bonanza_pd                  30, "Bonanza s01e19.mp4" and "BonanzaS02e17.mp4"
//	Shogun_Miniseries            4, "Shogun 1.mp4"
//	Bonanza_-_The_Trail_Gang     1, one episode filed on its own
//
// So a row here is a whole series, a miniseries, a single episode, or a
// bundle of two shows under one name. As a movie library it collapses a
// 169-episode run into one tile that plays episode one forever. Two rules
// hold this file up:
//
//   - READ THE FILE LIST AT SYNC, one round trip per title, in series and not
//     in parallel. That is the cost this library pays and the reason it is
//     the only show-shaped one.
//   - AN ITEM WITH NO PLAYABLE FILE IS SKIPPED ENTIRELY, no series row and no
//     episode rows. Since playableFiles() started matching `h.264 IA` this is
//     rare (none of the forty sampled items hit it), which is the point: the
//     skip is for an item that really holds nothing, not one whose format
//     string was spelled differently. It does not make the pass incomplete
//     either: we looked and it holds nothing, which is not the same as not
//     having looked.
package main

import (
	"strconv"
	"strings"
)

// maxShowPages holds this library to five hundred series, most-downloaded
// first, which is more classic television than a household will get through.
//
// It is 25 rather than 5 because the page size shrank, not because the
// ceiling moved: showRows is 20, so 25 pages is the same 500 titles. See
// showRows in query.go for why a page here cannot be a hundred rows.
const maxShowPages = 25

// maxPageItems is how many EPISODES one answer may carry, and it is the
// number this file got wrong first.
//
// The host clips a sync page at plugins.MaxSyncPageRows, which is 200, and
// it clips SILENTLY (internal/plugins/source.go: p.Items = p.Items[:200]).
// The first version of the curated pass returned all ten vouched shows in
// one answer, 391 episodes, and said `complete`. The host kept Green Acres
// (169), UFO (26) and five of Captain Scarlet's 38, and the other seven
// shows arrived as series rows with nothing in them. Worse than a missing
// shelf: the pass claimed to be complete, so the host was authorised to
// prune a library it had only half seen.
//
// This is the D174 lesson in a new place. A host SILENTLY DROPS what it
// cannot take, so a plugin handing over more than the contract allows does
// not get an error, it gets a quiet half. 180 leaves room under the
// ceiling rather than sitting on it.
const maxPageItems = 180

// showCursor is where a show-shaped pass resumes: which search page, which
// row of it, and how many of that row's episodes already went.
//
// The episode offset is the part a page-number cursor could not express,
// and it is why this is a struct. One archive item can be a whole series:
// Green Acres is 169 episodes, so a page boundary lands INSIDE an item
// often enough that resuming at item granularity would either drop the
// tail of a show or send its first hundred again forever.
//
// `blind` rides along because a plugin has no memory between calls. A page
// that could not read some item's file list has not seen everything, and
// the pass that ENDS is the one that reports `complete`, so the flag has
// to survive every page in between.
type showCursor struct {
	page, doc, ep int
	blind         bool
}

func (c showCursor) String() string {
	s := strconv.Itoa(c.page) + ":" + strconv.Itoa(c.doc) + ":" + strconv.Itoa(c.ep)
	if c.blind {
		s += ":b"
	}
	return s
}

// parseShowCursor reads a cursor back. An empty one is the first page,
// which is what the host sends to start a pass.
func parseShowCursor(s string) (showCursor, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return showCursor{page: 1}, true
	}
	parts := strings.Split(s, ":")
	if len(parts) < 3 || len(parts) > 4 {
		return showCursor{}, false
	}
	n := make([]int, 3)
	for i := 0; i < 3; i++ {
		v, err := strconv.Atoi(parts[i])
		if err != nil || v < 0 {
			return showCursor{}, false
		}
		n[i] = v
	}
	if n[0] < 1 {
		return showCursor{}, false
	}
	return showCursor{page: n[0], doc: n[1], ep: n[2], blind: len(parts) == 4 && parts[3] == "b"}, true
}

// showSource is one candidate for this library: the search row it came
// from, and the curated entry vouching for it if there is one.
type showSource struct {
	doc searchDoc
	p   *pick
}

// syncShows enumerates the show-shaped library, one bounded page at a time.
func syncShows(req syncRequest, l libDef) response {
	c, ok := parseShowCursor(req.Cursor)
	if !ok {
		return response{Error: "unreadable cursor " + req.Cursor}
	}
	if catalogDepth(req.Library.Config) == curatedOnly {
		return curatedShows(l, c)
	}
	var res searchResult
	if err := get(searchURLRows(l, req.Library.Config, c.page, showRows), &res); err != nil {
		// An honest failure changes nothing in the library, which is why a
		// sync that fails is different from a sync that returns less.
		return response{Error: "could not read " + l.name + ": " + err.Error()}
	}
	docs := res.Response.Docs
	if len(docs) == 0 && c.page == 1 {
		return response{Error: "that collection has no series with a playable copy"}
	}
	vouched := map[string]pick{}
	for _, v := range picksFor(l.key) {
		vouched[v.ia] = v
	}
	src := make([]showSource, 0, len(docs))
	for _, d := range docs {
		s := showSource{doc: d}
		if v, ok := vouched[strings.TrimSpace(d.Identifier)]; ok && v.tmdb != "" {
			pv := v
			s.p = &pv
		}
		src = append(src, s)
	}

	out, next, exhausted, blind := fillPage(src, c)
	blind = blind || c.blind
	if !exhausted {
		next.blind = blind
		out.Next = next.String()
		return response{Data: out}
	}
	// This search page is spent. Only an enumeration that really reached
	// the end of the result set may authorise the host to delete what it
	// did not see, and only if every item on the way was actually read.
	done := len(docs) < showRows || res.Response.Start+len(docs) >= res.Response.NumFound
	if done {
		out.Complete = !blind
		return response{Data: out}
	}
	if c.page < maxShowPages {
		out.Next = showCursor{page: c.page + 1, blind: blind}.String()
	}
	return response{Data: out}
}

// curatedShows is the default library: the shows this channel vouches for,
// and nothing the collection happens to hold beside them.
//
// It is also much the cheapest pass. Enumerating the collection costs one
// metadata call per candidate; this costs one per vouched show, so a first
// sync is ten round trips rather than five hundred. And because every one
// of them carries a TMDB series id, the host enriches the SERIES by id and
// then every episode against that series plus its season and episode
// number, which is what turns a folder of file names into a show with real
// episode titles and stills.
//
// It pages like the catalog pass, for the reason maxPageItems gives: ten
// shows is three hundred and ninety one episodes, and the host takes two
// hundred.
func curatedShows(l libDef, c showCursor) response {
	ps := picksFor(l.key)
	src := make([]showSource, 0, len(ps))
	for i := range ps {
		d := searchDoc{Identifier: ps[i].ia}
		d.Title.s = ps[i].title
		if ps[i].year > 0 {
			d.Year.s = strconv.Itoa(ps[i].year)
		}
		src = append(src, showSource{doc: d, p: &ps[i]})
	}
	out, next, exhausted, blind := fillPage(src, c)
	blind = blind || c.blind
	if !exhausted {
		next.blind = blind
		out.Next = next.String()
		return response{Data: out}
	}
	if len(out.Series) == 0 && c.doc == 0 {
		return response{Error: l.name + " has no curated shows this server could read, so there is nothing to list at this setting"}
	}
	// The table is finite and the pass walked all of it, so this is a real
	// enumeration and the host may prune against it. Unless something on
	// the way could not be read: an item that was never seen is not an item
	// that holds nothing.
	out.Complete = !blind
	return response{Data: out}
}

// fillPage expands sources from where the cursor left off until the page is
// full, and says which of the two things stopped it.
//
// A series row is repeated on every page that carries any of its episodes.
// That is deliberate and it is free: the host upserts a series by its
// external id, so the second copy lands on the row the first one made, and
// an episode arriving on page three still finds its show.
func fillPage(src []showSource, start showCursor) (out syncPage, next showCursor, exhausted, blind bool) {
	ep := start.ep
	for i := start.doc; i < len(src); i++ {
		if len(out.Items) >= maxPageItems {
			return out, showCursor{page: start.page, doc: i, ep: ep}, false, blind
		}
		s, eps, err := expandSeries(src[i].doc, src[i].p)
		if err != nil {
			// Never seen, rather than known to hold nothing, which is the
			// difference `complete` turns on.
			blind = true
			ep = 0
			continue
		}
		if ep >= len(eps) {
			ep = 0
			continue
		}
		take, room := eps[ep:], maxPageItems-len(out.Items)
		if len(take) > room {
			out.Series = append(out.Series, s)
			out.Items = append(out.Items, take[:room]...)
			return out, showCursor{page: start.page, doc: i, ep: ep + room}, false, blind
		}
		out.Series = append(out.Series, s)
		out.Items = append(out.Items, take...)
		ep = 0
	}
	return out, showCursor{}, true, blind
}

// expandSeries turns one search row into a series and its episodes. It
// returns nothing at all, and no error, for an item that holds no playable
// file: that is a decision, not a failure.
func expandSeries(d searchDoc, p *pick) (srcSeries, []srcItem, error) {
	id := strings.TrimSpace(d.Identifier)
	name := seriesName(d.Title.s)
	if id == "" || name == "" {
		return srcSeries{}, nil, nil
	}
	meta, err := itemMeta(id)
	if err != nil {
		return srcSeries{}, nil, err
	}
	files := meta.playableFiles()
	if len(files) == 0 {
		return srcSeries{}, nil, nil
	}
	s := srcSeries{
		ExternalID: id,
		Title:      name,
		Overview:   clip(strip(d.Description.s), maxOverview),
	}
	if y, err := strconv.Atoi(firstNumber(d.Year.s)); err == nil {
		s.Year = sensibleYear(y)
	}
	if p != nil {
		// A vouched-for series carries a TMDB SERIES id, so the enricher
		// looks the show up by id and then walks each EPISODE by its season
		// and number. That is what the parsing below is in aid of. No poster
		// either: the archive's own is 180x124 and enrichment replaces it a
		// moment later anyway.
		s.Meta = &metaRef{Provider: "tmdb", ID: p.tmdb}
	} else {
		s.Poster = "https://archive.org/services/img/" + pathEscape(id)
	}
	nums := numberEpisodes(files)
	titles := episodeTitles(files, name, nums)
	items := make([]srcItem, 0, len(files))
	for i, f := range files {
		items = append(items, srcItem{
			ExternalID: episodeID(id, f.Name),
			Title:      titles[i],
			Series:     id,
			Season:     nums[i].season,
			Episode:    nums[i].episode,
			RuntimeMin: fileMinutes(f.Length),
		})
	}
	return s, items, nil
}

// ---- the series row ----

// seriesJunk is what an uploader adds to a title to say the item is a bundle.
// It is true and it is not part of the show's name.
var seriesJunk = []string{
	"complete series", "complete docuseries", "complete collection",
	"all episodes collection", "all episodes", "full series", "tv series",
	"tv show", "miniseries", "mini series", "complete", "collection",
}

// seriesName is the item title with the bundling noise taken off, so a shelf
// reads "Green Acres", "UFO" and "Space 1999" rather than "Green Acres
// Complete Series", "UFO. (Complete Series)" and "Space 1999 (Complete
// Series 1)". It is also the key the episode titles are stripped against, so
// a bad clean here shows up twice.
func seriesName(raw string) string {
	full := strings.Join(strings.Fields(strip(raw)), " ")
	s := full
	for n := 0; n < 2; n++ {
		t := trimSeps(cutJunkTail(s))
		if t == s {
			break
		}
		s = t
	}
	if s == "" {
		return full // Everything the title said was noise, so noise it is.
	}
	return s
}

// cutJunkTail removes one trailing bundling phrase. A bracket is matched on
// its CONTENTS containing a phrase, so "(1995 miniseries)" and "(Complete
// Series 1)" go and "(1990)" stays. A bare tail has to end the title, because
// "Collection" mid-name is part of the name.
func cutJunkTail(s string) string {
	if s == "" {
		return s
	}
	low := strings.ToLower(s)
	if end := s[len(s)-1]; end == ')' || end == ']' {
		open := byte('(')
		if end == ']' {
			open = '['
		}
		i := strings.LastIndexByte(s, open)
		if i <= 0 {
			return s
		}
		for _, j := range seriesJunk {
			if strings.Contains(low[i+1:len(s)-1], j) {
				return s[:i]
			}
		}
		return s
	}
	for _, j := range seriesJunk {
		if len(s) > len(j) && strings.HasSuffix(low, j) {
			return s[:len(s)-len(j)]
		}
	}
	return s
}
