//go:build wasip1

// The catalog: a table, and the two ops that answer out of it.
//
// Everything below is data a real plugin would fetch. It is here instead
// because a fixture whose answers are known is the only way a harness can
// prove what a sync did to a library, and because a channel you can
// install with no account and no network is the fastest way for an author
// to see the family working before they write any of their own.
package main

import (
	"encoding/json"
	"strconv"
)

// ---- the tables ----

// film is one title in the Films library. A real plugin carries whatever
// id its service uses; these are ids a person can type into a harness.
type film struct {
	id      string
	title   string
	year    int
	minutes int
	genres  []string
	// cert gives the household rating cap something real to gate on. A
	// channel's items are ranked exactly as anything else in the library
	// is, which is ruling 3: the channel is where you browse, and the rest
	// of the app still knows the titles exist and still applies the cap.
	cert     string
	overview string
}

var films = []film{
	{"film-1", "The Long Weekend Cut", 1974, 96, []string{"Drama"}, "",
		"A projectionist keeps one reel back and the town rearranges itself around the gap."},
	{"film-2", "Static, and How to Read It", 1988, 104, []string{"Documentary"}, "",
		"Forty years of test cards, read as though somebody had meant them."},
	{"film-3", "Nothing Behind the Curtain", 2011, 88, []string{"Mystery"}, "",
		"A theatre with no film in it sells out for a year."},
	{"film-4", "A Sync in Two Pages", 2022, 111, []string{"Drama", "Comedy"}, "",
		"Everything arrives, but never all at once, and nobody agrees what that means."},
	{"film-5", "Grown-up Words About Codecs", 2024, 127, []string{"Drama"}, "R",
		"An argument about bitrates that outlives everyone having it."},
}

// show is one series in the Series library, with its episodes. Season 0
// holds the specials, which the library's own setting decides about.
type show struct {
	id       string
	title    string
	year     int
	overview string
	episodes []episode
}

type episode struct {
	season  int
	number  int
	title   string
	minutes int
	airDate string
}

var shows = []show{
	{"show-a", "The Fixture Channel", 2023,
		"Short films about how a plugin becomes a library.", []episode{
			{0, 1, "Before any of this worked", 6, "2023-01-01"},
			{1, 1, "Two libraries, one manifest", 24, "2023-03-05"},
			{1, 2, "The key is how you tell them apart", 22, "2023-03-12"},
			{1, 3, "What a layout owes the household", 26, "2023-03-19"},
		}},
	{"show-b", "Field Notes", 2021,
		"Somebody reads their own commit messages back, years later.", []episode{
			{0, 1, "An apology to the scanner", 8, "2021-06-01"},
			{1, 1, "The shelf that emptied itself", 31, "2021-09-02"},
			{1, 2, "Honest errors", 29, "2021-09-09"},
		}},
}

// ---- sync ----

// sync answers for ONE of this plugin's two libraries, told apart by the
// key the manifest gave it. A single-library plugin gets no key and is
// unaffected, which is the whole point of the widening.
func sync(raw json.RawMessage) response {
	var req syncRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable sync request"}
		}
	}
	switch req.Library.Key {
	case "films":
		return syncFilms(req)
	case "series":
		return syncSeries(req)
	case "":
		// A host that predates channels, or a bug. Either way this plugin
		// has two catalogs and no way to guess which was meant, so it says
		// so rather than picking one and filling the wrong library.
		return response{Error: "this plugin holds two libraries and the request did not say which"}
	}
	return response{Error: "no library called " + req.Library.Key + " in this channel"}
}

// syncFilms enumerates in two pages, because the paging rule is the part
// authors get wrong and one page would hide it.
//
// Page one carries the first three and a cursor. Page two carries the rest
// and claims completeness. `complete: true` says "I listed EVERYTHING",
// and it is the only thing that authorises the server to remove rows this
// pass did not mention. A fetch that failed halfway must leave it out: the
// library then stays exactly as it was, which is always better than
// emptying somebody's shelf because a service blinked.
func syncFilms(req syncRequest) response {
	list := filmsFor(req.Library.Config["era"])
	if req.Cursor == "" {
		if len(list) <= 3 {
			// The era setting narrowed it to one page. Hand back a cursor
			// only when there is something behind it: a second call that
			// returns nothing is a call nobody needed to make.
			return response{Data: syncPage{Items: filmItems(list, 0, len(list)), Complete: true}}
		}
		return response{Data: syncPage{
			Items: filmItems(list, 0, 3),
			Next:  "2",
		}}
	}
	return response{Data: syncPage{
		Items: filmItems(list, 3, len(list)),
		// The last page of a full enumeration, and only here.
		Complete: true,
	}}
}

// filmsFor applies the library's own `era` setting. A second Films library
// added from the same plugin can answer differently, because the setting
// belongs to the library row and not to the plugin.
func filmsFor(era string) []film {
	out := []film{}
	for _, f := range films {
		switch era {
		case "classics":
			if f.year >= 2000 {
				continue
			}
		case "new":
			if f.year < 2020 {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// filmItems turns rows from..to (half open) into items. What a source
// returns is an ITEM: a movie, exactly like one that came off a disk. That
// is why no client needed changing for any of this.
func filmItems(list []film, from, to int) []srcItem {
	out := []srcItem{}
	for i := from; i < to && i < len(list); i++ {
		f := list[i]
		it := srcItem{
			// externalId is IDENTITY. Keep it stable and a re-sync updates
			// the row it made last time, so progress, watched state and
			// playlists survive. Change it and you have made a second
			// title with an empty history.
			ExternalID:  f.id,
			Title:       f.title,
			Year:        f.year,
			Overview:    f.overview,
			ReleaseDate: strconv.Itoa(f.year) + "-05-01",
			RuntimeMin:  f.minutes,
			Genres:      f.genres,
			// A duration you declare is a hint for the shelf. The server
			// probes the stream when somebody presses Play and acts on
			// what it measured.
			DurationS: float64(f.minutes * 60),
		}
		if f.cert != "" {
			it.Certs = map[string]string{"US": f.cert}
		}
		out = append(out, it)
	}
	return out
}

// syncSeries answers in one page: two shows and their episodes, complete.
// A show-shaped library returns the series rows beside the items, and each
// item names its series by the same external id.
func syncSeries(req syncRequest) response {
	specials := req.Library.Config["includeSpecials"] == "true"
	series := []srcSeries{}
	items := []srcItem{}
	for _, s := range shows {
		series = append(series, srcSeries{
			ExternalID: s.id,
			Title:      s.title,
			Year:       s.year,
			Overview:   s.overview,
		})
		for _, e := range s.episodes {
			if e.season == 0 && !specials {
				continue
			}
			items = append(items, srcItem{
				ExternalID:  s.id + "-s" + strconv.Itoa(e.season) + "e" + strconv.Itoa(e.number),
				Title:       e.title,
				Overview:    "From " + s.title + ".",
				ReleaseDate: e.airDate,
				RuntimeMin:  e.minutes,
				Series:      s.id,
				Season:      e.season,
				Episode:     e.number,
				DurationS:   float64(e.minutes * 60),
			})
		}
	}
	// One page, and it listed everything, so it says so. Turning the
	// specials setting off is a real removal from this library and the
	// prune is meant to happen.
	return response{Data: syncPage{Series: series, Items: items, Complete: true}}
}

// ---- resolve ----

// resolve is asked for a stream when somebody presses Play, and this
// plugin has none: its catalog is a table and there is no media behind any
// row in it.
//
// So it returns an error with a sentence in it. The alternative, a
// made-up URL, is worse in a way that is easy to miss: the server would
// accept it, start a session, fail to fetch it and leave the person
// watching a spinner while it worked that out. An error arrives as a
// sentence on the screen instead, immediately, and says whose fault it is.
// That is D176's acceptance line: a resolver failure surfaces as an honest
// error, never a spinner.
//
// Everything a real resolver does is the two lines this one replaces: turn
// your id into a URL, say how long it is good for, and stop. See
// `examples/plugins/feeddemo` for one pointed at fixture media and
// `examples/plugins/archive` for one against a live service.
func resolve(raw json.RawMessage) response {
	var req resolveRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable resolve request"}
		}
	}
	// The key is read anyway, so the error can name the library and so
	// that a plugin copying this file has the two-catalog branch already
	// written where its own resolver goes.
	switch req.libraryKey() {
	case "films":
		return response{Error: "Channel demo has no film behind this title: its catalog is a fixture with no media."}
	case "series":
		return response{Error: "Channel demo has no episode behind this title: its catalog is a fixture with no media."}
	}
	return response{Error: "Channel demo has no media behind its catalog. It exists to show the shape of a channel, not to play anything."}
}
