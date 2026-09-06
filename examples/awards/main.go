//go:build wasip1

// Awards and nominations, from Wikidata. The reference for the shape most
// real plugins take: identify a title by an id the host already has, fetch
// something about it from a public source, cache the answer, and put it on
// a detail page and a page of its own.
//
// It is the only example that uses the `library` grant, and that grant is
// worth understanding. It does NOT let a plugin read the library. It answers
// exactly one question: "of these external ids, which do you hold?" So the
// person page can ask whether you own Oppenheimer without ever being able to
// ask what you own. The reply is refs, and the SERVER draws the cards, for
// the person looking, which is why a title above somebody's rating cap is
// simply not on their copy of the page.
//
// What it draws:
//
//	detail page   "Won 4 Academy Awards" plus a link to the page below
//	title page    every win and nomination for that film or show
//	person page   their awards, then the winning titles you already have as
//	              real cards, then the ones you do not as plain chips
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
package main

import (
	"encoding/json"
	"strconv"
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

func ok(v any) int64 {
	body, err := json.Marshal(map[string]any{"data": v})
	if err != nil {
		return fail("could not encode the answer")
	}
	return packed(body)
}

func fail(msg string) int64 {
	body, _ := json.Marshal(map[string]any{"error": msg})
	return packed(body)
}

func send(fn func(int32, int32) int64, v any) []byte {
	req, err := json.Marshal(v)
	if err != nil || len(req) == 0 {
		return nil
	}
	return unpack(fn(int32(uintptr(unsafe.Pointer(&req[0]))), int32(len(req))))
}

// ---- host functions ----
//
// Each of these exists ONLY because the manifest asked for it. An ungranted
// host function is not present in the module at all, so calling one is a
// link error at instantiation rather than a runtime refusal.

//go:wasmimport mp config
func hostConfig() int64

//go:wasmimport mp http
func hostHTTP(ptr, n int32) int64

//go:wasmimport mp storage
func hostStorage(ptr, n int32) int64

//go:wasmimport mp lookup
func hostLookup(ptr, n int32) int64

//go:wasmimport mp log
func hostLog(level, ptr, n int32)

func logf(msg string) {
	b := []byte(msg)
	if len(b) == 0 {
		return
	}
	hostLog(1, int32(uintptr(unsafe.Pointer(&b[0]))), int32(len(b)))
}

func settings() map[string]string {
	out := map[string]string{}
	json.Unmarshal(unpack(hostConfig()), &out)
	return out
}

// cacheGet/cacheSet wrap the storage grant. Wikidata's answers change on the
// order of once a year, and the alternative is a query per title per scan,
// so caching here is not an optimization: it is the difference between a
// polite plugin and one that gets a server throttled.
func cacheGet(key string) string {
	var out struct {
		Value string `json:"value"`
	}
	json.Unmarshal(send(hostStorage, map[string]any{"op": "get", "key": key}), &out)
	return out.Value
}

func cacheSet(key, value string) {
	send(hostStorage, map[string]any{"op": "set", "key": key, "value": value})
}

// mine asks the host which of these TMDB ids are in this library. The reply
// maps the ids it HOLDS onto refs; anything missing from the reply is simply
// not here. There is no way to phrase the opposite question.
func mine(tmdbIDs []string) map[string]map[string]any {
	if len(tmdbIDs) == 0 {
		return nil
	}
	if len(tmdbIDs) > 100 { // the host's per-call ceiling
		tmdbIDs = tmdbIDs[:100]
	}
	var out struct {
		Found map[string]map[string]any `json:"found"`
		Error string                    `json:"error"`
	}
	json.Unmarshal(send(hostLookup, map[string]any{"provider": "tmdb", "ids": tmdbIDs}), &out)
	if out.Error != "" {
		logf("lookup failed: " + out.Error)
		return nil
	}
	return out.Found
}

// ---- the families ----

// describeItem puts a ROW OF CIRCLES on a detail page (D174): one per
// award, wins first, the awarding body over the category, a check for a
// win and an open ring for a nomination.
//
// It used to be a fact row saying "5 wins, 21 nominations" and a button.
// The information was all there and none of it read at a glance, which is
// what the tile block exists to fix: the same data, in the shape the cast
// row above it already taught the reader.
//
// Episodes are skipped deliberately: awards belong to a film or a series,
// and querying once per episode would multiply a TV library's traffic by
// fifty for answers that do not exist.
func describeItem(in map[string]any) map[string]any {
	kind, _ := in["kind"].(string)
	provider, _ := in["metaProvider"].(string)
	metaID, _ := in["metaId"].(string)
	idf, _ := in["id"].(float64)
	id := int64(idf)
	if kind == "episode" || provider != "tmdb" || metaID == "" || id <= 0 {
		return map[string]any{}
	}
	all, err := fetch("title", metaID)
	if err != nil {
		// A source being down is not this item having no awards, and the
		// difference matters: returning nothing here would be cached as a
		// fact. Say so instead, and the host logs it without counting it
		// against the fault budget.
		return map[string]any{"blocks": []any{
			map[string]any{"type": "text", "text": "Awards could not be fetched just now."},
		}}
	}
	shown := keep(all)
	wins, noms := counts(shown)
	if wins == 0 && noms == 0 {
		return map[string]any{}
	}
	// No `fields` any more. A field becomes a fact row, and a fact row
	// saying what the circles already say is the clutter this change was
	// about. The tally survives as the row's fallback line, which is where
	// it is still needed.
	return map[string]any{"blocks": []any{awardRow(shown, id, wins, noms)}}
}

type subject struct {
	Kind string `json:"kind"`
	Item *struct {
		ID           int64  `json:"id"`
		Title        string `json:"title"`
		MetaID       string `json:"metaId"`
		MetaProvider string `json:"metaProvider"`
	} `json:"item,omitempty"`
	Person *struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"person,omitempty"`
}

func note(text string) map[string]any {
	return map[string]any{"blocks": []any{map[string]any{"type": "text", "text": text}}}
}

// titlePage lists every win and nomination for one title.
func titlePage(s *subject) map[string]any {
	if s == nil || s.Item == nil {
		return note("This title is no longer in the library.")
	}
	if s.Item.MetaProvider != "tmdb" || s.Item.MetaID == "" {
		return note("This title has not been matched to a metadata source, so there is nothing to look up yet.")
	}
	all, err := fetch("title", s.Item.MetaID)
	if err != nil {
		return note("Wikidata could not be reached: " + err.Error())
	}
	shown := keep(all)
	blocks := []any{
		map[string]any{"type": "heading", "text": s.Item.Title},
		// The title itself, as the app's own card. A ref, never a card: if
		// the person reading is not allowed to see it, the server drops the
		// block and the page renders without it.
		map[string]any{"type": "itemShelf", "refs": []any{map[string]any{"itemId": s.Item.ID}}},
	}
	wins, noms := counts(shown)
	if wins == 0 && noms == 0 {
		msg := "Wikidata lists no awards for this title."
		if len(all) > 0 {
			msg = "Nothing from the major ceremonies. Turn off \"Major ceremonies only\" in this plugin's settings to see the other " + plural(len(all), "award", "awards") + "."
		}
		blocks = append(blocks, map[string]any{"type": "text", "text": msg})
	} else {
		// The same tiles the detail row draws, stacked. A page that lists
		// what a row summarized should look like the row it came from, so
		// this is a layout change and not a different rendering.
		blocks = append(blocks,
			map[string]any{"type": "badges", "items": []string{tally(wins, noms)}},
			awardList(shown))
	}
	return map[string]any{"title": "Awards for " + s.Item.Title, "blocks": blocks}
}

// personPage lists what somebody won, then splits the winning titles into
// the ones this library holds and the ones it does not. That split is the
// whole point of the library grant: refs for what you have, plain chips for
// what you do not, and no way to ask what else is on the shelf.
func personPage(s *subject) map[string]any {
	if s == nil || s.Person == nil {
		return note("Nobody by that id has anything in this library.")
	}
	name := s.Person.Name
	if name == "" {
		name = "this person"
	}
	all, err := fetch("person", strconv.FormatInt(s.Person.ID, 10))
	if err != nil {
		return note("Wikidata could not be reached: " + err.Error())
	}
	shown := keep(all)
	blocks := []any{map[string]any{"type": "heading", "text": name}}
	if len(shown) == 0 {
		blocks = append(blocks, map[string]any{
			"type": "text",
			"text": "Wikidata lists no awards for " + name + ".",
		})
		return map[string]any{"title": "Awards", "blocks": blocks}
	}
	wins, _ := counts(shown)
	blocks = append(blocks,
		map[string]any{"type": "badges", "items": []string{plural(wins, "award", "awards")}},
		awardList(shown))

	// The winning works, split by what the library holds.
	var ids []string
	title := map[string]string{}
	seen := map[string]bool{}
	for _, a := range shown {
		if a.WorkTMDB == "" || seen[a.WorkTMDB] {
			continue
		}
		seen[a.WorkTMDB] = true
		ids = append(ids, a.WorkTMDB)
		title[a.WorkTMDB] = a.Work
	}
	held := mine(ids)
	var refs []any
	var missing []string
	for _, id := range ids {
		if ref, in := held[id]; in {
			refs = append(refs, ref)
			continue
		}
		if t := title[id]; t != "" {
			missing = append(missing, t)
		}
	}
	if len(refs) > 0 {
		blocks = append(blocks, map[string]any{
			"type": "itemShelf",
			"text": "Award-winning work in your library",
			"refs": refs,
		})
	}
	if len(missing) > 0 {
		blocks = append(blocks, map[string]any{
			"type":  "badges",
			"text":  "Awarded, but not in your library",
			"items": missing,
		})
	}
	return map[string]any{"title": "Awards for " + name, "blocks": blocks}
}

//go:wasmexport mp_handle
func mpHandle(ptr, length int32) int64 {
	// Release the previous call's buffers. mp_alloc is driven by the HOST:
	// once for this call's input, and again for every host-function reply
	// (config, http, storage, lookup). Nothing else ever frees them, so
	// without this line the guest's memory only grows, and a backfill over a
	// real library dies on "out of memory" after a few dozen items. By the
	// time a new call arrives the host has finished reading the last reply,
	// so everything except the input just written is done with.
	for k := range live {
		if k != ptr {
			delete(live, k)
		}
	}
	var call struct {
		Op   string          `json:"op"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(read(ptr, length), &call); err != nil {
		return fail("could not read the call: " + err.Error())
	}
	switch call.Op {
	case "describe.item":
		in := map[string]any{}
		json.Unmarshal(call.Data, &in)
		return ok(describeItem(in))
	case "page.render":
		var req struct {
			PageID  string   `json:"pageId"`
			Subject *subject `json:"subject"`
		}
		if err := json.Unmarshal(call.Data, &req); err != nil {
			return fail("could not read the page request: " + err.Error())
		}
		switch req.PageID {
		case "title":
			return ok(titlePage(req.Subject))
		case "person":
			return ok(personPage(req.Subject))
		}
		return fail("this plugin does not serve the page " + req.PageID)
	}
	return fail("this plugin does not implement " + call.Op)
}

// main is never called: the module is a reactor, started through
// _initialize, and every entry point is an export.
func main() {}
