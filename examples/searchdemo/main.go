//go:build wasip1

// The D165 search-family reference: a search hook with no network at all.
//
// A real hook asks a service what it has. This one answers out of a table
// it carries, which is what makes it a fixture: the harness knows exactly
// which query produces which rows, and the family's promises can be proven
// without a third party being up.
//
// The shape worth copying:
//
//   - a result is a TILE, the same typed row the tiles block is built
//     from: art, a title, a subtitle, and one of the host's own actions.
//     Nothing here describes how anything is drawn.
//   - answer null when you have nothing. That is the common answer, and it
//     costs the person searching nothing at all.
//   - link somewhere you own. A row whose action opens one of YOUR pages
//     works on every surface and needs no permissions; a row that opens a
//     local title needs the `library` grant to know its id.
//   - be fast, and expect to be dropped when you are not. The host waits a
//     quarter of a second, keeps your answer when it arrives late, and
//     shows it a keystroke later. `slowMs` below exists to make that
//     visible, and no real plugin should have anything like it.
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

// ---- the plugin ----

// series is one thing this plugin knows about: a run of films people look
// for by a name that is not on any of them.
type series struct {
	Name    string
	Aliases []string
	Titles  []string
}

// The whole database. A real hook queries a service; the point of this one
// is that the answer is predictable, so a harness can assert it.
var known = []series{
	{
		Name:    "The Middle-earth films",
		Aliases: []string{"lotr", "middle earth", "tolkien", "rings"},
		Titles: []string{
			"The Fellowship of the Ring", "The Two Towers", "The Return of the King",
		},
	},
	{
		Name:    "The Blade Runner films",
		Aliases: []string{"blade runner", "replicant", "dick"},
		Titles:  []string{"Blade Runner", "Blade Runner 2049"},
	},
	{
		Name:    "The Alien films",
		Aliases: []string{"alien", "xenomorph", "ripley"},
		Titles:  []string{"Alien", "Aliens", "Alien 3", "Alien Resurrection"},
	},
}

// tile is one result row. The field names are the host's: this is the same
// shape a tiles block carries, which is why a search section needed no new
// renderer on any client.
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

// link is a host verb. `page` opens one of this plugin's own declared
// pages, which is the one action a plugin can always offer: it needs no
// permissions and it cannot point anywhere else.
type link struct {
	Kind   string            `json:"kind"`
	PageID string            `json:"pageId,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// find matches a query against what this plugin knows. The host already
// trimmed, folded and clipped it, so there is nothing to normalize here.
func find(q string) *series {
	if len(q) < 3 {
		return nil // too short to mean anything, and cheap to decline
	}
	for i := range known {
		if strings.Contains(strings.ToLower(known[i].Name), q) {
			return &known[i]
		}
		for _, alias := range known[i].Aliases {
			if strings.Contains(alias, q) || strings.Contains(q, alias) {
				return &known[i]
			}
		}
	}
	return nil
}

// results turns a match into rows.
func results(s *series) []tile {
	out := make([]tile, 0, len(s.Titles))
	for _, title := range s.Titles {
		out = append(out, tile{
			Title:    title,
			Subtitle: s.Name,
			Art:      art{Glyph: "film", Tint: "accent"},
			Action: &link{
				Kind: "page", PageID: "series", Params: map[string]string{"name": s.Name},
			},
		})
	}
	return out
}

// slow is the demonstration knob: it makes this plugin miss the host's
// window on purpose, so an owner (and the harness) can see what a slow
// provider actually costs. Nothing real should ever do this.
func slow() {
	ms, err := strconv.Atoi(strings.TrimSpace(settings()["slowMs"]))
	if err != nil || ms <= 0 {
		return
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

// page draws the page a result row links to.
func page(name string) map[string]any {
	for i := range known {
		if known[i].Name != name {
			continue
		}
		return map[string]any{
			"title": known[i].Name,
			"blocks": []any{
				map[string]any{
					"type": "text",
					"text": "Found by the search demo. These are the titles it knows under that name, whether or not this server has them.",
				},
				map[string]any{"type": "badges", "items": known[i].Titles},
			},
		}
	}
	// A page for something this plugin no longer knows is an empty page,
	// not an error: the link may be older than the table.
	return map[string]any{"title": "Not found", "blocks": []any{
		map[string]any{"type": "text", "text": "Nothing here by that name any more."},
	}}
}

//go:wasmexport mp_handle
func mpHandle(ptr, n int32) int64 {
	for k := range live {
		if k != ptr {
			delete(live, k)
		}
	}
	var req request
	if err := json.Unmarshal(read(ptr, n), &req); err != nil {
		return respond(response{Error: "malformed request"})
	}
	switch req.Op {
	case "search.query":
		var q struct {
			Q string `json:"q"`
		}
		if err := json.Unmarshal(req.Data, &q); err != nil {
			return respond(response{Error: "malformed query"})
		}
		slow()
		match := find(q.Q)
		if match == nil {
			// Null is "nothing for that", which is what a search hook says
			// most of the time. No section is drawn.
			return respond(response{Data: nil})
		}
		return respond(response{Data: map[string]any{
			"layout": "list", "results": results(match),
		}})
	case "page.render":
		var p struct {
			PageID string            `json:"pageId"`
			Params map[string]string `json:"params"`
		}
		if err := json.Unmarshal(req.Data, &p); err != nil {
			return respond(response{Error: "malformed page request"})
		}
		if p.PageID != "series" {
			return respond(response{Error: "this plugin does not serve the page " + p.PageID})
		}
		return respond(response{Data: page(p.Params["name"])})
	}
	return respond(response{Error: "this plugin does not handle " + req.Op})
}

func main() {}
