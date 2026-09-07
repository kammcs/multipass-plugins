//go:build wasip1

// The D176 source-family reference: a library, and the stream behind it.
//
// A real source plugin fetches a catalog and asks its service for a stream
// when somebody presses Play. This one carries five uploads of one channel
// in a table and is TOLD where its media lives, which is what makes it a
// fixture: the harness knows exactly what a sync should produce and can
// serve the media itself, so the family's promises can be proven without a
// service being up.
//
// The shape worth copying:
//
//   - what you return is an ITEM. Not a video, not a stream, not a card: a
//     movie or an episode, exactly like one that came off a disk. That is
//     why nothing on any client needed changing for this family.
//   - `externalId` is IDENTITY. Keep it stable and a re-sync updates the
//     row it made last time, so somebody's progress, watched state and
//     playlists survive. Change it and you have made a second title.
//   - answer in pages with `next`, and say `complete: true` only when you
//     have listed EVERYTHING. That flag is the only thing that lets the
//     server remove what you did not list. If your fetch failed halfway,
//     leave it out: the library stays exactly as it was, which is always
//     better than emptying somebody's shelf because a service blinked.
//   - a duration you declare is a hint. The server probes the stream when
//     somebody presses Play, and what it measures is what it acts on.
//   - a source can answer the search box too, and `find` below is the
//     whole composition: ask the host which of your ids are already items
//     here, and send those rows to the item itself. The viewer gets their
//     own card, with its progress and its Play button, from a plugin that
//     never saw a local id until it asked for one.
//
// The `catalog` and `resolve` settings exist so the harness can drive
// every ending each op has: four for a sync (full, shorter, never
// completing, failing) and three for Play (a stream, a failure, and an
// address this plugin never asked permission for). No real plugin has
// anything like them.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

// ---- ABI boilerplate (see examples/plugins/hello for the walkthrough) ----

var live = map[int32][]byte{}

//go:wasmexport mp_alloc
func mpAlloc(size int32) int32 {
	if size <= 0 {
		size = 1
	}
	buf := make([]byte, size)
	ptr := int32(uintptr(unsafe.Pointer(&buf[0])))
	live[ptr] = buf
	return ptr
}

func read(ptr, n int32) []byte {
	if n <= 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), n)
}

func packed(data []byte) int64 {
	ptr := mpAlloc(int32(len(data)))
	copy(live[ptr], data)
	return int64(ptr)<<32 | int64(len(data))
}

func unpack(v int64) []byte {
	if v == 0 {
		return nil
	}
	return read(int32(v>>32), int32(v))
}

//go:wasmimport mp config
func hostConfig() int64

//go:wasmimport mp lookup
func hostLookup(ptr, n int32) int64

func settings() map[string]string {
	out := map[string]string{}
	json.Unmarshal(unpack(hostConfig()), &out)
	return out
}

type request struct {
	Op   string          `json:"op"`
	Data json.RawMessage `json:"data,omitempty"`
}

type response struct {
	Error string `json:"error,omitempty"`
	Data  any    `json:"data,omitempty"`
}

func respond(r response) int64 {
	out, err := json.Marshal(r)
	if err != nil {
		return packed([]byte(`{"error":"could not encode the reply"}`))
	}
	return packed(out)
}

// ---- the contract ----

// syncRequest is what the host asks. `config` is what the owner filled in
// when they added the library, so two libraries from this plugin can name
// different channels.
type syncRequest struct {
	Library struct {
		Shape  string            `json:"shape"`
		Config map[string]string `json:"config,omitempty"`
	} `json:"library"`
	Cursor string `json:"cursor,omitempty"`
	Since  string `json:"since,omitempty"`
}

type syncPage struct {
	Series   []srcSeries `json:"series,omitempty"`
	Items    []srcItem   `json:"items,omitempty"`
	Next     string      `json:"next,omitempty"`
	Complete bool        `json:"complete,omitempty"`
}

type srcSeries struct {
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	Overview   string `json:"overview,omitempty"`
}

type srcItem struct {
	ExternalID  string            `json:"externalId"`
	Title       string            `json:"title"`
	Overview    string            `json:"overview,omitempty"`
	ReleaseDate string            `json:"releaseDate,omitempty"`
	RuntimeMin  int               `json:"runtimeMin,omitempty"`
	Series      string            `json:"series,omitempty"`
	Season      int               `json:"season,omitempty"`
	Episode     int               `json:"episode,omitempty"`
	Certs       map[string]string `json:"certs,omitempty"`
	DurationS   float64           `json:"durationS,omitempty"`
}

// resolveRequest is what the host asks when somebody presses Play. The
// item arrives in YOUR terms: the id you filed it under.
type resolveRequest struct {
	Library struct {
		Shape  string            `json:"shape"`
		Config map[string]string `json:"config,omitempty"`
	} `json:"library"`
	Item struct {
		ExternalID string `json:"externalId"`
		Series     string `json:"series,omitempty"`
		Season     int    `json:"season,omitempty"`
		Episode    int    `json:"episode,omitempty"`
		Title      string `json:"title,omitempty"`
	} `json:"item"`
}

type resolved struct {
	URL       string            `json:"url"`
	Headers   map[string]string `json:"headers,omitempty"`
	ExpiresAt int64             `json:"expiresAt,omitempty"`
	Hint      string            `json:"hint,omitempty"`
}

// ---- the catalog ----

const channelID = "chan-demo"

// upload is one thing this plugin publishes. A real one would carry the
// id its service uses; this one uses ids a harness can type.
type upload struct {
	id      string
	title   string
	minutes int
	cert    string
}

var uploads = []upload{
	{"vid-1", "Making a library out of nothing", 12, ""},
	{"vid-2", "What a sync is allowed to delete", 9, ""},
	{"vid-3", "Identity, and why it is not the row", 14, ""},
	{"vid-4", "The probe is the authority", 7, ""},
	// The last one carries a certification, so a household rating cap has
	// something real to gate on: a plugin's items are ranked exactly as
	// anything else in the library is.
	{"vid-5", "Grown-up words about codecs", 21, "TV-MA"},
}

// mode says how this sync should end. It is the whole reason the setting
// exists, and it is the part no real plugin copies.
func mode() string {
	m := settings()["catalog"]
	if m == "" {
		return "full"
	}
	return m
}

func channelName(req syncRequest) string {
	if n := req.Library.Config["channel"]; n != "" {
		return n
	}
	return "The Demo Channel"
}

// sync answers one page. Page one carries the channel and the first three
// uploads; page two carries the rest and closes the enumeration.
func sync(raw json.RawMessage) response {
	var req syncRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable sync request"}
		}
	}
	m := mode()
	if m == "error" {
		// An honest failure. The host records it, shows it in the hub, and
		// changes nothing in the library.
		return response{Error: "the demo catalog is unavailable"}
	}
	count := len(uploads)
	if m == "trimmed" || m == "incomplete" {
		// trimmed: the last upload really is gone from the service.
		// incomplete: this sync only ever GOT that far, which is a
		// different thing entirely, and the flag below is the difference.
		count--
	}

	if req.Cursor == "" {
		return response{Data: syncPage{
			Series: []srcSeries{{
				ExternalID: channelID,
				Title:      channelName(req),
				Year:       2024,
				Overview:   "Five short films about how a plugin becomes a library.",
			}},
			Items: page(1, min(3, count)),
			Next:  "2",
		}}
	}
	return response{Data: syncPage{
		Items: page(4, count),
		// `incomplete` is a sync that ran out of catalog without saying so:
		// the host must leave the library alone rather than treating a
		// short answer as "everything else is gone".
		Complete: m != "incomplete",
	}}
}

// page returns the uploads numbered from..to, one-based and inclusive.
func page(from, to int) []srcItem {
	out := []srcItem{}
	for i := from; i <= to && i <= len(uploads); i++ {
		u := uploads[i-1]
		it := srcItem{
			ExternalID:  u.id,
			Title:       u.title,
			Overview:    "Episode " + strconv.Itoa(i) + " of the demo channel.",
			ReleaseDate: "2024-0" + strconv.Itoa(i) + "-01",
			RuntimeMin:  u.minutes,
			Series:      channelID,
			Season:      2024,
			Episode:     i,
			DurationS:   float64(u.minutes * 60),
		}
		if u.cert != "" {
			it.Certs = map[string]string{"US": u.cert}
		}
		out = append(out, it)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// resolveCount makes each resolution visibly different, so a harness can
// tell a fresh one from a cached one. A real plugin has no need for it: it
// returns whatever its service just handed it.
var resolveCount int

// resolve hands back a stream. Everything below the switch is the whole of
// what a real resolver does: turn your id into a URL, say how long it is
// good for, and stop.
//
// The URL is fetched by the SERVER, which probes it and gives the viewer
// its own HLS session, so nothing here reaches a device. That is also why
// the host refuses an address outside the hosts this plugin asked
// permission for, and refuses plain http anywhere but loopback.
func resolve(raw json.RawMessage) response {
	var req resolveRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable resolve request"}
		}
	}
	switch settings()["resolve"] {
	case "error":
		// What a service being down looks like. The person sees a sentence
		// saying this plugin could not provide the stream, not a spinner.
		return response{Error: "the demo cannot reach its media right now"}
	case "badhost":
		// A host this plugin never asked permission for. The server refuses
		// it before fetching anything, which is the point of the grant.
		return response{Data: resolved{URL: "https://example.com/not-ours.mp4"}}
	}
	// A service that wants a token on the fetch says so here, and the
	// server puts it on the request it makes. The header never leaves this
	// server: no device sees it, and neither does anything else.
	var headers map[string]string
	if settings()["resolve"] == "auth" {
		headers = map[string]string{"X-Demo-Token": "hunter2"}
	}
	base := req.Library.Config["mediaBase"]
	if base == "" {
		return response{Error: "this library has no media location configured"}
	}
	ttl := int64(900)
	if v, err := strconv.ParseInt(settings()["ttlSeconds"], 10, 64); err == nil && v > 0 {
		ttl = v
	}
	resolveCount++
	return response{Data: resolved{
		URL:       base + req.Item.ExternalID + ".mp4?n=" + strconv.Itoa(resolveCount),
		Headers:   headers,
		ExpiresAt: time.Now().Unix() + ttl,
		Hint:      "mp4",
	}}
}

// ---- finding what this library already holds ----

// The composition (PLUGIN-LIBRARIES section 8), in about thirty lines: a
// source plugin that also answers the search box asks the host which of
// ITS ids are already items here, and returns rows that open those items.
// The viewer gets their own library's card, with its progress and its
// Play button, from a plugin that never saw a local id until it asked.
//
// The host answers in this plugin's own namespace only, and only about
// libraries the person asking can see. A search answer is cached for the
// whole house, so a call with nobody behind it is told about shared
// libraries and nothing else: a plugin cannot put one person's private
// library into an answer everybody gets.
type lookupOp struct {
	Provider string   `json:"provider"`
	IDs      []string `json:"ids"`
}

type ref struct {
	ItemID   int64 `json:"itemId,omitempty"`
	SeriesID int64 `json:"seriesId,omitempty"`
}

type lookupResult struct {
	Found map[string]ref `json:"found,omitempty"`
	Error string         `json:"error,omitempty"`
}

type tile struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Art      art    `json:"art,omitempty"`
	Action   *link  `json:"action,omitempty"`
}

type art struct {
	Glyph string `json:"glyph,omitempty"`
	Tint  string `json:"tint,omitempty"`
}

type link struct {
	Kind     string `json:"kind"`
	ItemID   int64  `json:"itemId,omitempty"`
	SeriesID int64  `json:"seriesId,omitempty"`
}

func find(q string) response {
	q = strings.TrimSpace(strings.ToLower(q))
	if len(q) < 3 {
		return response{} // too short to mean anything, and cheap to decline
	}
	ids := []string{}
	matched := []upload{}
	for _, u := range uploads {
		if strings.Contains(strings.ToLower(u.title), q) {
			matched = append(matched, u)
			ids = append(ids, u.id)
		}
	}
	if len(matched) == 0 {
		return response{}
	}
	req, err := json.Marshal(lookupOp{Provider: "theater.multipass.feeddemo", IDs: ids})
	if err != nil {
		return response{}
	}
	ptr := mpAlloc(int32(len(req)))
	copy(live[ptr], req)
	var found lookupResult
	json.Unmarshal(unpack(hostLookup(ptr, int32(len(req)))), &found)

	rows := []tile{}
	for _, u := range matched {
		r, ok := found.Found[u.id]
		if !ok || r.ItemID == 0 {
			// Not in a library this person can see. A row with nowhere to
			// go is not a row, so it is left out rather than drawn dead.
			continue
		}
		rows = append(rows, tile{
			Title:    u.title,
			Subtitle: "From the demo channel",
			Art:      art{Glyph: "film", Tint: "teal"},
			Action:   &link{Kind: "item", ItemID: r.ItemID},
		})
	}
	if len(rows) == 0 {
		return response{}
	}
	return response{Data: map[string]any{"layout": "list", "results": rows}}
}

//go:wasmexport mp_handle
func mpHandle(ptr, n int32) int64 {
	var req request
	if err := json.Unmarshal(read(ptr, n), &req); err != nil {
		return respond(response{Error: "unreadable request"})
	}
	switch req.Op {
	case "source.sync":
		return respond(sync(req.Data))
	case "source.resolve":
		return respond(resolve(req.Data))
	case "search.query":
		var q struct {
			Q string `json:"q"`
		}
		json.Unmarshal(req.Data, &q)
		return respond(find(q.Q))
	}
	return respond(response{Error: "unsupported op " + req.Op})
}

func main() {}
