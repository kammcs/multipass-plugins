//go:build wasip1

// The D165 reference MATCHING plugin: it implements the matching family
// (docs/architecture/plugins.md) without needing a network, so you can see
// plugin matching work before writing one that talks to a real service.
//
// It matches everything, inventing metadata from the title it was given.
// That makes it useful for two things: trying the feature, and being the
// deterministic fixture the plugin harness drives.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
//	multipass plugins pack . -out myplugin.mpp
//
// Unsigned: that is what developer mode is for. Registry packages are
// signed on acceptance, so you never need a key to write or test one.
package main

import (
	"encoding/json"
	"strings"
	"unsafe"
)

// ---- the ABI boilerplate (identical in every Go plugin; an SDK will hide
// this, but a reference plugin should show the whole thing) ----

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

// config is the host function every plugin may call: the owner-edited
// settings this plugin declared in its manifest.
//
//go:wasmimport mp config
func hostConfig() int64

func settings() map[string]string {
	out := map[string]string{}
	v := hostConfig()
	if v == 0 {
		return out
	}
	json.Unmarshal(read(int32(v>>32), int32(v)), &out)
	return out
}

type request struct {
	Op   string          `json:"op"`
	Data json.RawMessage `json:"data,omitempty"`
}

// reply sends data back. A nil payload becomes JSON null, which the host
// reads as "I have nothing" (a no-match), distinct from an error.
func reply(v any) int64 {
	out, err := json.Marshal(map[string]any{"data": v})
	if err != nil {
		return packed([]byte(`{"error":"could not encode the reply"}`))
	}
	return packed(out)
}

// ---- the matching family ----

type args struct {
	Title    string `json:"title"`
	Name     string `json:"name"`
	Query    string `json:"query"`
	ID       string `json:"id"`
	SeriesID string `json:"seriesId"`
	Kind     string `json:"kind"`
	Year     int    `json:"year"`
	Season   int    `json:"season"`
	Episode  int    `json:"episode"`
	// describe.item extras (D165 phase 2): the host hands over a summary of
	// the item so a plugin never has to read the library.
	Overview     string `json:"overview"`
	MetaID       string `json:"metaId"`
	MetaProvider string `json:"metaProvider"`
	RuntimeMin   int    `json:"runtimeMin"`
	Genres       string `json:"genres"`
	Locale       string `json:"locale"`
}

// fact is one key/value row, and block is one unit of the display
// vocabulary. A plugin that only fills in fields gets a native-looking
// section on all seven surfaces without writing a single block.
type fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type link struct {
	Label  string         `json:"label"`
	Action map[string]any `json:"action"`
}

type block struct {
	Type         string   `json:"type"`
	Text         string   `json:"text,omitempty"`
	Rows         []fact   `json:"rows,omitempty"`
	Items        []string `json:"items,omitempty"`
	Links        []link   `json:"links,omitempty"`
	FallbackText string   `json:"fallbackText,omitempty"`
}

type meta struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Overview    string   `json:"overview,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	Year        int      `json:"year,omitempty"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Cast        []cast   `json:"cast,omitempty"`
}

type cast struct {
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
	PersonID  int64  `json:"personId,omitempty"`
}

// slug makes a stable id out of a title, so re-matching the same title
// lands on the same id (the host re-fetches by id, and an id that moved
// would look like a different title every time).
func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "untitled"
	}
	return out
}

func invent(title string, year int) *meta {
	if title == "" {
		return nil // nothing to go on: a no-match, not a guess
	}
	if year == 0 {
		year = 1999
	}
	return &meta{
		ID:          "demo-" + slug(title),
		Title:       title,
		Overview:    settings()["note"],
		Genres:      []string{"Demo"},
		Rating:      7.5,
		Year:        year,
		// Deliberately NO runtime. The host cross-checks a searched match
		// against the file's real duration (D142) and rejects one that
		// disagrees by more than about half, so a matcher that invents a
		// runtime rejects its own matches on short files. Report a runtime
		// only when the service you are asking actually knows it.
		Cast: []cast{
			{Name: "Ada Placeholder", Character: "Herself", PersonID: 1},
		},
	}
}

// pad2 renders a day number as two digits, so the invented air dates are
// well-formed rather than "2001-01-3".
func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d []byte
	for n > 0 {
		d = append([]byte{byte('0' + n%10)}, d...)
		n /= 10
	}
	return string(d)
}

// unslug turns an id back into a title, so a by-id lookup answers with the
// same thing the original match did.
func unslug(id string) string {
	return strings.TrimPrefix(id, "demo-")
}

//go:wasmexport mp_handle
func mpHandle(ptr, n int32) int64 {
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
	var req request
	if err := json.Unmarshal(read(ptr, n), &req); err != nil {
		return packed([]byte(`{"error":"malformed request"}`))
	}
	var a args
	json.Unmarshal(req.Data, &a)

	switch req.Op {
	case "match.movie":
		return reply(invent(a.Title, a.Year))
	case "match.series":
		return reply(invent(a.Name, a.Year))
	case "match.episode":
		// A real matching service answers with the episode's NAME, which is
		// what the host uses to title the item. Inventing one from the
		// numbers keeps this deterministic for the harness while showing
		// the shape: id, title, and an air date.
		if a.Season <= 0 || a.Episode <= 0 {
			return reply(nil) // specials and unnumbered files: no honest answer
		}
		return reply(&meta{
			ID:          a.SeriesID + "-s" + itoa(a.Season) + "e" + itoa(a.Episode),
			Title:       "The Demo Episode " + itoa(a.Episode),
			Overview:    settings()["note"],
			ReleaseDate: "2001-01-" + pad2(a.Episode),
			Year:        2001,
		})
	case "match.movieById", "match.seriesById":
		return reply(invent(unslug(a.ID), 0))
	case "match.search":
		m := invent(a.Query, a.Year)
		if m == nil {
			return reply([]any{})
		}
		return reply([]map[string]any{{
			"id": m.ID, "title": m.Title, "year": m.Year, "overview": m.Overview,
		}})
	case "match.images", "match.person":
		return reply(nil) // this plugin has no artwork and no people

	case "describe.item":
		// Everything this plugin knows about the item, in the two halves the
		// host accepts: plain fields (rendered as a fact list for free) and
		// blocks for anything richer.
		if a.Title == "" {
			return reply(nil) // nothing to say is a normal answer
		}
		fields := []fact{
			{Label: "Demo id", Value: slug(a.Title)},
			{Label: "Matched by", Value: a.MetaProvider},
		}
		if a.RuntimeMin > 0 {
			fields = append(fields, fact{Label: "Length", Value: itoa(a.RuntimeMin) + " min"})
		}
		blocks := []block{
			{Type: "text", Text: "This section came from a plugin. " + settings()["note"]},
			{Type: "badges", Items: []string{"Demo", strings.ToUpper(a.Kind)}},
			{Type: "links", Links: []link{{
				Label:  "About Multipass plugins",
				Action: map[string]any{"kind": "url", "url": "https://multipass.theater/plugins"},
			}}},
			// A type from a future vocabulary. The host drops what it does not
			// know, so this proves the forward-compat rule rather than
			// breaking the section.
			{Type: "timeline.v2", FallbackText: "A future block type."},
		}
		return reply(map[string]any{"fields": fields, "blocks": blocks})
	}
	return packed([]byte(`{"error":"this plugin does not handle that request"}`))
}

func main() {}
