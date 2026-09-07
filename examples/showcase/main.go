//go:build wasip1

// The catalogue. One plugin that draws every shape and widget the block
// vocabulary has, so an author can find the one they want, see what it
// looks like on their own server, and copy the twenty lines that made it.
//
// It exists because the vocabulary is now big enough that reading the
// reference alone leaves you guessing. A `tiles` block has four layouts
// and four shapes, and the combinations do not all read the same way: a
// wide card in a list is a thumbnail with two lines beside it, the same
// wide card in a row is a shelf card, and on web-TV a wide GRID is a row
// because `.wgrid` is a flex row there by existing rule. No amount of
// prose settles that. A page you can look at does.
//
// Like `examples/plugins/pagedemo`, this one asks for NO PERMISSIONS AT
// ALL. It never needs them: a page declares a SUBJECT, and the host
// resolves it before calling in, so the plugin arrives already holding a
// real item id. That is what lets the showcase draw real posters and a
// real backdrop instead of coloured rectangles, without ever reading the
// library. Copy `examples/plugins/awards` when you are building something
// that fetches; copy this when you are deciding what a block should look
// like.
//
// Two tiles here deliberately do things the host refuses, and say so in a
// comment where they do it (search this file's companion, gallery.go, for
// "the host"). A refusal you can see in a live page is worth more than a
// rule you have to remember.
//
// The page builders live in gallery.go. This file is the ABI, the call
// dispatch, and the detail-page section.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
//
// Without -buildmode=c-shared the module is a COMMAND, not a reactor, and
// refuses every call the host makes.
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

// ---- what the host hands over ----

// item is the resolved subject of a `subject: item` page: the facts the
// host looked up before calling in. Only the fields this plugin prints
// are declared, because a plugin that decodes fields it never reads goes
// stale for no reason.
type item struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	RuntimeMin int    `json:"runtimeMin"`
	Genres     string `json:"genres"`
}

type subject struct {
	Kind string `json:"kind"`
	Item *item  `json:"item,omitempty"`
}

// itoa64 keeps the page builders free of strconv noise. Page params are
// strings on the wire, so an item id turns into one more often than you
// would expect.
func itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// itoa is the same for the small numbers a fact list prints.
func itoa(n int) string { return strconv.Itoa(n) }

// ---- the detail-page section ----

// describeItem is what a film or series gains when this plugin is
// installed: one line saying what the page is, with the button that opens
// it, and a short row showing what a `wide` shelf card looks like in the
// place an author is most likely to want one.
//
// A detail-page section is not the place to show everything. It sits
// under somebody's film, so it gets two blocks and gets out of the way.
func describeItem(in map[string]any) map[string]any {
	idf, _ := in["id"].(float64)
	id := int64(idf)
	if id <= 0 {
		return map[string]any{} // nothing to link to
	}
	openGallery := map[string]any{
		"kind":   "page",
		"pageId": "gallery",
		"params": map[string]string{"id": itoa64(id)},
	}
	return map[string]any{
		"blocks": []any{
			// A notice is one line of state with at most one button. It is
			// the block for "here is where you stand", which is why the
			// tone is a WORD and not a colour: each client picks the
			// colour out of the viewer's theme, so the same notice reads
			// right on a dark TV skin and a light phone.
			map[string]any{
				"type": "notice",
				"tone": "info",
				"text": "Every block shape on one page, with the source beside it.",
				"links": []any{map[string]any{
					"label":  "Open the gallery",
					"action": openGallery,
				}},
				// An older client does not know the `notice` type and
				// draws this line instead. Every block in this plugin sets
				// one, because that is the entire forward-compatibility
				// contract: the plugin says what the block MEANT, and a
				// client that cannot draw it still says something true.
				"fallbackText": "Showcase: every block shape on one page.",
			},
			// The 16:9 shelf card, in the layout most sections want. Note
			// there is no art here beyond a monogram and a tint: nothing
			// usable in `art` draws a tinted panel with the title set
			// large, which is a real shape and not an error state.
			map[string]any{
				"type":   "tiles",
				"layout": "row",
				"shape":  "wide",
				"text":   "Three wide cards",
				"tiles": []any{
					map[string]any{
						"title":    "Hero",
						"subtitle": "One tile, full width, dots",
						"badge":    "NEW",
						"art":      map[string]any{"monogram": "H", "tint": "accent"},
						"action":   openGallery,
					},
					map[string]any{
						"title":    "Stats and bars",
						"subtitle": "Figures worth reading at a glance",
						"art":      map[string]any{"glyph": "clock", "tint": "blue"},
						"action":   openGallery,
					},
					map[string]any{
						"title":    "Two columns",
						"subtitle": "Side by side on a screen with room",
						"art":      map[string]any{"glyph": "folder", "tint": "green"},
						"action":   openGallery,
					},
				},
				"fallbackText": "Hero, stats, bars, columns and every tile shape.",
			},
		},
	}
}

// ---- dispatch ----

//go:wasmexport mp_handle
func mpHandle(ptr, length int32) int64 {
	// Release the previous call's buffers. mp_alloc is driven by the HOST
	// and nothing else ever frees them, so without this the guest's memory
	// only grows. By the time a new call arrives the host has finished
	// reading the last reply, so everything except the input just written
	// is done with.
	for k := range live {
		if k != ptr {
			delete(live, k)
		}
	}
	var call struct {
		Op string `json:"op"`
		// The envelope is {op, data}. Not {op, input}: that mistake costs
		// an afternoon because the call succeeds and the payload is empty.
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
		if req.PageID == "gallery" {
			return ok(galleryPage(req.Subject))
		}
		return fail("this plugin does not serve the page " + req.PageID)
	}
	return fail("this plugin does not implement " + call.Op)
}

// main is never called: the module is a reactor, started through
// _initialize, and every entry point is an export.
func main() {}
