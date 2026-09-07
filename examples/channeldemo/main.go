//go:build wasip1

// The D177 stage 2 reference: a channel, which is several libraries and a
// page that arrives with them.
//
// A channel is not a new kind of content. Its libraries are D176 source
// libraries, its items are ordinary movies and episodes, and its page is
// the block vocabulary every other plugin already draws in. What the
// family adds is that all of it ships together: one manifest declares the
// card on the Channels shelf, the libraries behind it, and the layout the
// page opens with.
//
// This one carries its catalog in a table and reaches nothing. That is
// deliberate, and it is the same posture as `examples/plugins/feeddemo`:
// a harness knows exactly what a sync should produce, and an owner who
// installs it out of curiosity gets a channel that works with nothing to
// configure and nothing to sign into. Copy `examples/plugins/archive` for
// what a source plugin against a live service looks like.
//
// The shape worth copying:
//
//   - a channel's libraries are told apart by KEY. The manifest names
//     them (`films`, `series`) and every sync and every resolve says which
//     one it is for. A plugin with one library never sees a key and needs
//     no change, which is why every shipped source plugin stayed correct.
//   - each library carries its OWN settings, filled in on the add form
//     under its own name. Films has an era; Series has a specials toggle;
//     neither can read the other's.
//   - `complete: true` is a claim that you listed EVERYTHING, and it is
//     the only thing that lets the server remove what you did not list.
//     Films answers in two pages and claims it on the second. See sync in
//     catalog.go for what a half-finished enumeration must do instead.
//   - a resolver with nothing behind it returns an ERROR, not a URL. See
//     resolve in catalog.go for why that is the honest answer.
//   - `channel.render` fills the one `slot` the layout declares. A slot is
//     the only part of the page the plugin draws; everything else is a
//     host-filled block the owner can retune or hide without ever calling
//     in here.
//
// What this plugin does NOT ask for is as much of the lesson as what it
// does: no capabilities at all. No outbound host, no storage, no library
// lookup. A channel that carries its own catalog needs none of them, and
// an install screen that says "asks for nothing" is worth more than any
// feature it would have bought.
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

// request is the call envelope: {op, data}. Not {op, input}, which is the
// mistake every author makes once.
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

// syncLibrary is which library the host is asking about. `key` is the
// channel addition: it is the key this plugin's manifest gave the library,
// so a plugin holding several catalogs knows which one it is enumerating.
// It is absent for a single-library source plugin.
type syncLibrary struct {
	Key    string            `json:"key,omitempty"`
	Shape  string            `json:"shape"`
	Config map[string]string `json:"config,omitempty"`
}

type syncRequest struct {
	Library syncLibrary `json:"library"`
	Cursor  string      `json:"cursor,omitempty"`
	Since   string      `json:"since,omitempty"`
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
	Year        int               `json:"year,omitempty"`
	Overview    string            `json:"overview,omitempty"`
	ReleaseDate string            `json:"releaseDate,omitempty"`
	RuntimeMin  int               `json:"runtimeMin,omitempty"`
	Genres      []string          `json:"genres,omitempty"`
	Series      string            `json:"series,omitempty"`
	Season      int               `json:"season,omitempty"`
	Episode     int               `json:"episode,omitempty"`
	Certs       map[string]string `json:"certs,omitempty"`
	DurationS   float64           `json:"durationS,omitempty"`
}

// resolveRequest is what arrives when somebody presses Play. The item is
// in YOUR terms: the id you filed it under.
//
// The library key is read from two places on purpose. The plan says
// "SyncLibrary gains key, and the item handed to source.resolve carries
// library: {key}", which can be read as the request's own library block or
// as one nested on the item. Reading whichever is filled costs four lines
// and cannot be wrong either way.
type resolveRequest struct {
	Library syncLibrary `json:"library"`
	Item    struct {
		ExternalID string `json:"externalId"`
		Series     string `json:"series,omitempty"`
		Season     int    `json:"season,omitempty"`
		Episode    int    `json:"episode,omitempty"`
		Title      string `json:"title,omitempty"`
		Library    *struct {
			Key string `json:"key,omitempty"`
		} `json:"library,omitempty"`
	} `json:"item"`
}

func (r resolveRequest) libraryKey() string {
	if r.Library.Key != "" {
		return r.Library.Key
	}
	if r.Item.Library != nil {
		return r.Item.Library.Key
	}
	return ""
}

// renderRequest is the channel family's one op. `slot` is the id of the
// slot block in the layout; `locale` is the viewer's language; `viewer` is
// present only for a plugin with per-profile settings, which this one does
// not have.
type renderRequest struct {
	Slot   string `json:"slot"`
	Locale string `json:"locale,omitempty"`
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
	case "channel.render":
		return respond(render(req.Data))
	}
	return respond(response{Error: "unsupported op " + req.Op})
}

func main() {}
