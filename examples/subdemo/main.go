//go:build wasip1

// The D165 subtitles-family reference: a subtitle provider with no network
// at all.
//
// A real provider searches a service and downloads a file. This one
// generates a short, correct WebVTT from the item it was asked about,
// which is what makes it a fixture: the harness can assert the exact text
// that lands on disk, and the family's promises can be proven without a
// third party being up.
//
// The shape worth copying:
//
//   - search is cheap and fetch is not, so they are two ops. The owner
//     reads the candidates and picks one; nothing downloads on its own.
//   - `id` is yours. The host hands it straight back and never parses it.
//   - `release` is the most useful thing on a row. It is how a person tells
//     the extended cut's subtitles from the theatrical one's.
//   - `content` must be valid UTF-8. Decode whatever your source sent
//     before you return it: a host that guessed would serve mojibake under
//     your name, and this one refuses instead.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
package main

import (
	"encoding/json"
	"strconv"
	"strings"
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

// query is what a search asks. The item is the same shape every family
// that gets one receives; the languages are the owner's.
type query struct {
	Item struct {
		ID    int64  `json:"id"`
		Kind  string `json:"kind"`
		Title string `json:"title"`
		Year  int    `json:"year"`
	} `json:"item"`
	Languages []string `json:"languages"`
}

// candidate is one row of the search.
type candidate struct {
	ID              string  `json:"id"`
	Lang            string  `json:"lang"`
	Title           string  `json:"title,omitempty"`
	Format          string  `json:"format,omitempty"`
	Release         string  `json:"release,omitempty"`
	Forced          bool    `json:"forced,omitempty"`
	HearingImpaired bool    `json:"hearingImpaired,omitempty"`
	Rating          float64 `json:"rating,omitempty"`
	Downloads       int     `json:"downloads,omitempty"`
}

// releases are the two cuts this fixture pretends to have found. Two,
// rather than one, because the whole reason a person reads this list is to
// tell them apart, and a provider that returns a single row teaches
// nothing about why the family works the way it does.
var releases = []struct {
	tag  string
	name string
}{
	{"theatrical", "Theatrical.1080p.BluRay"},
	{"extended", "Extended.Edition.2160p.WEB"},
}

// languages is what the owner gets when they ask for nothing in
// particular. A real provider answers with whatever its service has.
var languages = []string{"en", "es"}

func wanted(q query) []string {
	if len(q.Languages) == 0 {
		return languages
	}
	var out []string
	for _, want := range q.Languages {
		for _, have := range languages {
			if strings.EqualFold(want, have) {
				out = append(out, have)
			}
		}
	}
	return out
}

func search(q query) []candidate {
	var out []candidate
	for _, lang := range wanted(q) {
		for i, rel := range releases {
			out = append(out, candidate{
				// The id is ours, and it carries everything fetch needs.
				// A real provider would use its own row id here.
				ID:      lang + ":" + rel.tag + ":" + strconv.FormatInt(q.Item.ID, 10),
				Lang:    lang,
				Title:   strings.ToUpper(lang),
				Format:  "vtt",
				Release: rel.name,
				Rating:  9 - float64(i),
				// The second cut is marked SDH so a picker has something
				// to show beyond the release name.
				HearingImpaired: i == 1,
			})
		}
	}
	return out
}

// fetchOne builds the track. The text names the cut it was timed against,
// which is what lets a test prove that the file on disk is the candidate
// somebody picked rather than whichever one happened to be first.
func fetchOne(id string) (string, bool) {
	parts := strings.Split(id, ":")
	if len(parts) != 3 {
		return "", false
	}
	lang, cut := parts[0], parts[1]
	var body strings.Builder
	body.WriteString("WEBVTT\n\n")
	body.WriteString("1\n00:00:01.000 --> 00:00:04.000\n")
	body.WriteString(strings.ToUpper(lang) + " subtitles for the " + cut + " cut\n\n")
	body.WriteString("2\n00:00:05.000 --> 00:00:08.000\n")
	body.WriteString("Supplied by the subtitle demo plugin\n")
	return body.String(), true
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
	case "subtitles.search":
		var q query
		if err := json.Unmarshal(req.Data, &q); err != nil {
			return respond(response{Error: "malformed query"})
		}
		found := search(q)
		if len(found) == 0 {
			// Null is "nothing for this title", which is a normal answer
			// and not an error.
			return respond(response{Data: nil})
		}
		return respond(response{Data: map[string]any{"subtitles": found}})
	case "subtitles.fetch":
		var in struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(req.Data, &in); err != nil {
			return respond(response{Error: "malformed request"})
		}
		text, ok := fetchOne(in.ID)
		if !ok {
			return respond(response{Error: "no such subtitle"})
		}
		return respond(response{Data: map[string]any{"content": text, "format": "vtt"}})
	}
	return respond(response{Error: "this plugin does not handle " + req.Op})
}

func main() {}
