//go:build wasip1

// The movie-shaped sync: curated rows first, then the catalog tail.
//
// The order is the feature. A channel library that fills in `downloads
// desc` order fills with whatever the archive's viewers watched most, and
// section 3 of docs/ARCHIVE-CHANNEL.md is what that actually returns. So
// page one emits the vouched-for titles before it emits a single catalog
// row: the good films exist first, they carry TMDB ids so the host enriches
// them by id, and the shelves are already worth looking at while the tail
// is still arriving.
package main

import (
	"encoding/json"
	"strconv"
	"strings"
)

// maxPages is where this plugin stops asking. Twenty pages is 2,000 titles,
// which is a library somebody can scroll rather than a database. Stopping
// here is why `Complete` below is conditional.
const maxPages = 20

func sync(raw json.RawMessage) response {
	var req syncRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable sync request"}
		}
	}
	key := strings.TrimSpace(req.Library.Key)
	if key == "" {
		// A single-source plugin has one catalog and its sync carries no
		// key. This one is a channel: every library it owns is declared in
		// the manifest's channel block with a key, and without that key
		// there is no way to know which of the eight is being asked for.
		return response{Error: "this sync carries no library key, and every library this channel owns is identified by one"}
	}
	l, ok := libByKey(key)
	if !ok {
		return response{Error: "this plugin has no library called " + key}
	}
	if l.shape == "show" {
		// Classic Television is one metadata call per title, because an
		// item there is a whole series, or a miniseries, or one episode, or
		// nothing playable. shows.go owns that.
		return syncShows(req, l)
	}
	return syncMovies(req, l)
}

func syncMovies(req syncRequest, l libDef) response {
	// The default library is the vouched-for table and nothing else, and it
	// is a COMPLETE enumeration: the table is finite and this pass listed
	// all of it, so the host may prune, which is the one depth where a
	// title dropped from the table actually leaves the library.
	if catalogDepth(req.Library.Config) == curatedOnly {
		// Reported even though it finishes in milliseconds. An owner
		// watching the activity list sees one row per plugin, and the
		// library that said nothing at all is the one that looks stuck.
		report(progressReport{Label: "Listing " + l.name, Detail: "the curated list"})
		items := []srcItem{}
		for _, p := range picksFor(l.key) {
			items = append(items, curatedItem(p))
		}
		reportDone()
		if len(items) == 0 {
			return response{Error: l.name + " has no curated titles yet, so there is nothing to list at this setting"}
		}
		return response{Data: syncPage{Items: items, Complete: true}}
	}
	page := 1
	if req.Cursor != "" {
		n, err := strconv.Atoi(req.Cursor)
		if err != nil || n < 1 {
			return response{Error: "unreadable cursor " + req.Cursor}
		}
		page = n
	}
	// The row goes up BEFORE the request, because the request is where the
	// time goes: a report written after it would only ever describe work
	// that had already finished.
	w := walk{label: "Listing " + l.name}
	w.at(0, "reading page "+strconv.Itoa(page))
	var res searchResult
	if err := get(searchURL(l, req.Library.Config, page), &res); err != nil {
		// An honest failure. The host records it, shows it in the hub, and
		// changes nothing in the library, which is the whole reason a sync
		// that fails is different from a sync that returns less.
		reportDone()
		return response{Error: "could not read " + l.name + ": " + err.Error()}
	}
	docs := res.Response.Docs
	if len(docs) == 0 && page == 1 {
		reportDone()
		// This plugin wrote the query, so an empty first page means the
		// archive renamed or emptied a collection. Reporting it is not just
		// tidier than filing the curated rows alone: an empty page is a
		// COMPLETE page by the test below, and a complete page holding
		// twelve rows would authorise the host to delete the rest.
		return response{Error: l.name + " came back empty, which means that collection has moved"}
	}

	items := []srcItem{}
	if page == 1 {
		for _, p := range picksFor(l.key) {
			items = append(items, curatedItem(p))
		}
	}
	for _, d := range docs {
		if it, ok := item(d); ok {
			items = append(items, it)
		}
	}

	// The end of the result set, or the end of this plugin's patience. Only
	// the first of those has listed everything, and only the first may
	// authorise the host to delete what it did not see. A truncated pass
	// leaves the library growing only, which is the right way round: a
	// shelf with a stale title on it beats a shelf somebody's film vanished
	// from.
	seen := res.Response.Start + len(docs)
	done := len(docs) < rows || seen >= res.Response.NumFound
	out := syncPage{Items: items, Complete: done}
	if !done && page < maxPages {
		out.Next = strconv.Itoa(page + 1)
	}
	if out.Next == "" {
		// Nothing follows this page, whether because the pass listed
		// everything or because it reached this plugin's own ceiling, so
		// the row goes now rather than ageing out ninety seconds later
		// over a library that has finished.
		reportDone()
	} else {
		w.total = res.Response.NumFound
		w.at(seen, strconv.Itoa(seen)+" of "+strconv.Itoa(res.Response.NumFound)+" titles")
	}
	return response{Data: out}
}

// curatedItem is one row of the table this channel vouches for.
//
// It sends `meta` and nothing else that the metadata service will send
// better: no poster, because the archive's own is 180x124 and enrichment
// overwrites it a moment later anyway, and no overview, because TMDB's
// arrives by id and is written for a person rather than for a card
// catalogue. Sending nothing keeps a stretched thumbnail from being what
// somebody sees in the window before enrichment lands.
func curatedItem(p pick) srcItem {
	it := srcItem{
		ExternalID: p.ia,
		Title:      p.title,
		Year:       sensibleYear(p.year),
	}
	if p.tmdb != "" {
		it.Meta = &metaRef{Provider: "tmdb", ID: p.tmdb}
	}
	return it
}

// item maps one search index row to a catalog title.
func item(d searchDoc) (srcItem, bool) {
	id := strings.TrimSpace(d.Identifier)
	title := strings.TrimSpace(d.Title.s)
	if id == "" || title == "" {
		return srcItem{}, false
	}
	if curated(id) {
		// Page one already emitted a better version of this title, with a
		// TMDB id on it. Emitting it again here would overwrite that with
		// the archive's thumbnail and the archive's description.
		return srcItem{}, false
	}
	it := srcItem{
		ExternalID: id,
		Title:      title,
		Overview:   clip(strings.TrimSpace(strip(d.Description.s)), maxOverview),
		RuntimeMin: minutes(d.Runtime.s),
		// An uncurated row DOES send the thumbnail: it is 180x124 and it is
		// the only picture this title will ever have, and a small picture
		// beats a letterbox. This is the CDN-served address, which names no
		// file: the server's cache reads the format off the response. The
		// copy beside the item's own files goes through a redirect and is
		// about three times slower, which over 2,000 titles is minutes.
		Poster: "https://archive.org/services/img/" + pathEscape(id),
	}
	// `year` is the archive's commonest lie: a recent upload carries the
	// UPLOAD year, so a 1982 film reads 2026. sensibleYear is the bound
	// that catches the garbage; a wrong-but-plausible year survives it, and
	// that is why nothing here sorts on this field.
	if y, err := strconv.Atoi(firstNumber(d.Year.s)); err == nil {
		it.Year = sensibleYear(y)
	}
	return it, true
}
