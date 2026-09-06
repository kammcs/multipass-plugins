//go:build wasip1

// The smallest complete plugin page, and the one thing no other example can
// show: a page that asks for NO PERMISSIONS AT ALL.
//
// That is not a trick. A page declares a SUBJECT, and the host resolves it
// before calling the plugin, so a page about an item arrives already holding
// the item's facts and everybody credited on it. A plugin that would
// otherwise need to read the library needs no grant to draw a real page
// about one. Compare `examples/plugins/awards`, which is the example to copy
// when you are building something real: it fetches, caches and looks titles
// up, and asks for three grants to do it.
//
// So this page deliberately renders what it was GIVEN, rather than inventing
// a feature. It is a demo, it says so, and it doubles as the reference for
// the two container block types (itemShelf and personList): you hand back
// IDS and the server draws the cards, for the person looking.
//
// It used to serve a crew page and a director page too. D166 moved directors
// into the product proper (every detail page has a Director shelf, and the
// person page behind it has a real bio), so those pages were duplicating a
// built-in badly and are gone.
//
// It is also the reference for settings v2. One setting here is scoped to
// the PROFILE, so two people in the same house see this page differently,
// and two buttons show both action scopes: one the owner presses in their
// plugin card, one each person presses on their own settings screen.
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

// config is the host function every plugin may call, no grant needed. What
// comes back is already merged for whoever the call is for: the owner's
// household values, with this person's own on top of anything the manifest
// marked scope: profile. A plugin never asks whose values these are, and
// cannot: that is the same privacy line the events family draws.
//
//go:wasmimport mp config
func hostConfig() int64

func unpack(v int64) []byte {
	if v == 0 {
		return nil
	}
	return read(int32(v>>32), int32(v))
}

func settings() map[string]string {
	out := map[string]string{}
	json.Unmarshal(unpack(hostConfig()), &out)
	return out
}

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

// ---- the plugin ----

// maxPeople caps the person row. The host hands over everyone credited on a
// title, which for a big film is dozens; a demo should show the block type,
// not empty a database onto the screen.
const maxPeople = 8

// credit is one row of the host's resolved subject. The field names are the
// contract's.
type credit struct {
	PersonID int64  `json:"personId,omitempty"`
	Name     string `json:"name,omitempty"`
	Kind     string `json:"kind,omitempty"` // cast | crew
	Job      string `json:"job,omitempty"`
}

type subject struct {
	Kind string `json:"kind"`
	Item *struct {
		ID           int64    `json:"id"`
		Kind         string   `json:"kind"`
		Title        string   `json:"title"`
		Year         int      `json:"year"`
		Overview     string   `json:"overview"`
		MetaID       string   `json:"metaId"`
		MetaProvider string   `json:"metaProvider"`
		RuntimeMin   int      `json:"runtimeMin"`
		Genres       string   `json:"genres"`
		People       []credit `json:"people"`
	} `json:"item,omitempty"`
}

// describeItem puts one link on a detail page. This is the fields family
// doing the smallest useful thing: return a link block pointing at a page
// this same plugin serves.
func describeItem(in map[string]any) map[string]any {
	idf, _ := in["id"].(float64)
	id := int64(idf)
	if id <= 0 {
		return map[string]any{} // nothing to link to
	}
	return map[string]any{
		"blocks": []any{
			map[string]any{
				"type": "links",
				"links": []any{map[string]any{
					"label": "Inside a plugin page",
					"action": map[string]any{
						"kind":   "page",
						"pageId": "demo",
						"params": map[string]string{"id": strconv.FormatInt(id, 10)},
					},
				}},
			},
		},
	}
}

func fact(label, value string) map[string]any {
	return map[string]any{"label": label, "value": value}
}

// demoPage renders the subject the host resolved, and says so.
func demoPage(s *subject) map[string]any {
	// The per-profile setting. Nothing here is conditional on WHO is
	// looking, only on what they chose, which is the whole idea: the host
	// caches this page per viewer precisely because this line exists.
	// Absent means full: a demo that hides its own content by default
	// would be teaching the wrong thing.
	full := settings()["detail"] != "short"
	if s == nil || s.Item == nil {
		// A subject that has gone is not an error. Say so plainly rather
		// than returning nothing, which would render as a blank page.
		return map[string]any{
			"blocks": []any{
				map[string]any{"type": "text", "text": "This title is no longer in the library."},
			},
		}
	}
	it := s.Item

	rows := []any{fact("Kind", it.Kind)}
	if !full {
		// The short view: the person reading asked for less. Their
		// housemate can ask for more and get a different page.
		rows = []any{fact("Kind", it.Kind), fact("Detail", "short (change this on your own settings screen)")}
	}
	if full && it.Year > 0 {
		rows = append(rows, fact("Year", strconv.Itoa(it.Year)))
	}
	if full && it.RuntimeMin > 0 {
		rows = append(rows, fact("Runtime", strconv.Itoa(it.RuntimeMin)+" min"))
	}
	if full && it.Genres != "" {
		rows = append(rows, fact("Genres", it.Genres))
	}
	if full && it.MetaProvider != "" && it.MetaID != "" {
		// The id an author keys off to look this title up somewhere else.
		rows = append(rows, fact("Matched as", it.MetaProvider+":"+it.MetaID))
	}
	if full {
		rows = append(rows, fact("People credited", strconv.Itoa(len(it.People))))
	}

	blocks := []any{
		map[string]any{"type": "heading", "text": it.Title},
		// The title this page is about, drawn as the app's own card. Note
		// that we pass a REF and not a card: if the person reading is not
		// allowed to see this title, the server drops the whole block and
		// the page renders without it. That is not something a plugin can
		// get wrong, because it is not something a plugin decides.
		map[string]any{
			"type": "itemShelf",
			"refs": []any{map[string]any{"itemId": it.ID}},
		},
		map[string]any{
			"type": "text",
			"text": "Everything below arrived with the request. This plugin asks for no permissions at all: the page declared its subject, and the host resolved it before calling in.",
		},
		map[string]any{"type": "factList", "rows": rows},
	}

	// The second container block. Same rule as the shelf: ids in, cards out.
	var people []int64
	seen := map[int64]bool{}
	for _, c := range it.People {
		if c.PersonID <= 0 || seen[c.PersonID] {
			continue
		}
		seen[c.PersonID] = true
		people = append(people, c.PersonID)
		if len(people) == maxPeople {
			break
		}
	}
	if len(people) > 0 {
		blocks = append(blocks, map[string]any{
			"type":      "personList",
			"text":      "Some of the people the host named",
			"personIds": people,
		})
	}
	return map[string]any{"title": "Inside a plugin page: " + it.Title, "blocks": blocks}
}

// action answers a button press with one sentence, which is the entire
// contract: an action may not return blocks. The owner's button and the
// per-person button are told apart by the manifest, not by the plugin, and
// only the per-person one is told who pressed it.
func action(id string, personal bool, who string) map[string]any {
	switch id {
	case "ping":
		return map[string]any{"ok": true, "message": "The plugin answered. Nothing was contacted: this one has no grants."}
	case "mine":
		if !personal {
			return map[string]any{"ok": false, "message": "That button is a per-person one."}
		}
		detail := settings()["detail"]
		if detail == "" {
			detail = "short"
		}
		if who == "" {
			who = "You"
		}
		return map[string]any{"ok": true, "message": who + " sees the " + detail + " version of this page."}
	}
	return map[string]any{"ok": false, "message": "This plugin has no button called " + id + "."}
}

//go:wasmexport mp_handle
func mpHandle(ptr, length int32) int64 {
	// Release the previous call's buffers. mp_alloc is driven by the HOST:
	// once for this call's input, and again for every host-function reply
	// (config, http, storage, lookup). Nothing else ever frees them, so
	// without this the guest's memory only grows, and a long run over a real
	// library dies on "out of memory". By the time a new call arrives the
	// host has finished reading the last reply, so everything except the
	// input just written is done with.
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
		if req.PageID == "demo" {
			return ok(demoPage(req.Subject))
		}
		return fail("this plugin does not serve the page " + req.PageID)
	case "settings.action":
		var req struct {
			Action  string `json:"action"`
			Profile *struct {
				Name string `json:"name"`
			} `json:"profile,omitempty"`
		}
		json.Unmarshal(call.Data, &req)
		who := ""
		if req.Profile != nil {
			who = req.Profile.Name
		}
		return ok(action(req.Action, req.Profile != nil, who))
	}
	return fail("this plugin does not implement " + call.Op)
}

// main is never called: the module is a reactor, started through
// _initialize, and every entry point is an export.
func main() {}
