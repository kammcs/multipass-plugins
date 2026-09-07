//go:build wasip1

// The D176 reference for a library that belongs to a PERSON, and for how
// the families compose into one product.
//
// feeddemo next door is a source anyone in the house shares, added like a
// folder. This one is the other kind: a service each person signs in to.
// Connecting an account is what creates their library (ruling 5), so there
// is no "add library" step and no owner-side setting that could start it
// for somebody else. Disconnecting removes it again.
//
// It declares four families, which is the point: this is what the phase-5
// scenario actually looks like when it is built.
//
//   - events, for the connect flow. It carries no events at all, which a
//     plugin is allowed to do only when its libraries belong to a person:
//     what it wants is the Connect button, not a scrobble feed.
//   - source, for the catalog and the streams.
//   - search, so typing a title finds things this service has, next to the
//     viewer's own results.
//   - pages, so a row that is NOT in the library yet has somewhere to go.
//
// The composition worth copying is in `find`: a search result asks the
// host whether an id this plugin knows is already an item HERE
// (`lookup`, under the `library` grant, in this plugin's own namespace).
// If it is, the row opens the item, with its progress, its rating chip and
// its Play button. If it is not, the row opens one of this plugin's own
// pages. One list, two destinations, and the host draws both.
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

// ---- the catalog ----

const pluginID = "theater.multipass.catalogdemo"

// show is one thing this service publishes. A real plugin fetches this
// per account; this one carries it, so a harness can assert exactly what a
// sync produces.
type show struct {
	id      string
	title   string
	minutes int
}

var shows = []show{
	{"cd-1", "Everything Is A Library", 11},
	{"cd-2", "The Item With No File", 8},
	{"cd-3", "Who The Library Belongs To", 13},
	// Search knows about this one and a sync never returns it, which is
	// what a catalog bigger than somebody's account looks like: the row
	// has to lead to one of this plugin's own pages, because there is no
	// item to open.
	{"cd-4", "The Library You Do Not Have", 6},
}

// inAccount is what a sync returns: everything but the last, so the
// difference between "yours" and "ours" is visible.
func inAccount() []show { return shows[:len(shows)-1] }

// ---- connecting ----

// linkState is whatever this plugin needs to finish the flow. The host
// stores it, never reads it, and never returns it through the API, so a
// real token is safe here.
type linkState struct {
	Code  string `json:"code"`
	Polls int    `json:"polls"`
}

type linkStart struct {
	Instructions string          `json:"instructions"`
	URL          string          `json:"url"`
	Code         string          `json:"code"`
	ExpiresIn    int             `json:"expiresIn"`
	Interval     int             `json:"interval"`
	State        json.RawMessage `json:"state,omitempty"`
}

type linkResult struct {
	Pending bool            `json:"pending"`
	Linked  bool            `json:"linked"`
	Account string          `json:"account,omitempty"`
	State   json.RawMessage `json:"state,omitempty"`
}

func linkBegin() response {
	code := "DEMO-" + strconv.FormatInt(time.Now().Unix()%10000, 10)
	state, _ := json.Marshal(linkState{Code: code})
	return response{Data: linkStart{
		Instructions: "Open the address below on your phone and type this code to connect your account.",
		URL:          "https://multipass.theater/plugins",
		Code:         code,
		ExpiresIn:    300,
		Interval:     3,
		State:        state,
	}}
}

// linkPoll finishes after as many polls as the owner configured, so a
// harness can drive both the waiting and the finishing. A real plugin asks
// its service whether the person typed the code yet.
func linkPoll(raw json.RawMessage) response {
	var in struct {
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
		State json.RawMessage `json:"state"`
	}
	json.Unmarshal(raw, &in)
	var st linkState
	json.Unmarshal(in.State, &st)
	st.Polls++
	needed := 1
	if v, err := strconv.Atoi(settings()["pollsToConnect"]); err == nil && v > 0 {
		needed = v
	}
	state, _ := json.Marshal(st)
	if st.Polls < needed {
		return response{Data: linkResult{Pending: true, State: state}}
	}
	account := in.Profile.Name
	if account == "" {
		account = "demo account"
	}
	return response{Data: linkResult{Linked: true, Account: account, State: state}}
}

// ---- the library ----

type syncRequest struct {
	Library struct {
		Shape  string            `json:"shape"`
		Config map[string]string `json:"config,omitempty"`
	} `json:"library"`
	Cursor string `json:"cursor,omitempty"`
}

type syncPage struct {
	Items    []srcItem `json:"items,omitempty"`
	Next     string    `json:"next,omitempty"`
	Complete bool      `json:"complete,omitempty"`
}

type srcItem struct {
	ExternalID string  `json:"externalId"`
	Title      string  `json:"title"`
	Overview   string  `json:"overview,omitempty"`
	RuntimeMin int     `json:"runtimeMin,omitempty"`
	Year       int     `json:"year,omitempty"`
	DurationS  float64 `json:"durationS,omitempty"`
}

// sync produces a movie-shaped library: every title in the account, in one
// page, because three is not a catalog worth paginating.
func sync() response {
	items := []srcItem{}
	for _, s := range inAccount() {
		items = append(items, srcItem{
			ExternalID: s.id,
			Title:      s.title,
			Overview:   "One of the demo service's own films.",
			RuntimeMin: s.minutes,
			Year:       2024,
			DurationS:  float64(s.minutes * 60),
		})
	}
	return response{Data: syncPage{Items: items, Complete: true}}
}

type resolved struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

func resolve(raw json.RawMessage) response {
	var req struct {
		Library struct {
			Config map[string]string `json:"config,omitempty"`
		} `json:"library"`
		Item struct {
			ExternalID string `json:"externalId"`
		} `json:"item"`
	}
	json.Unmarshal(raw, &req)
	base := settings()["mediaBase"]
	if base == "" {
		return response{Error: "this plugin has not been told where its media is"}
	}
	return response{Data: resolved{
		URL:       base + req.Item.ExternalID + ".mp4",
		ExpiresAt: time.Now().Unix() + 900,
		Hint:      "mp4",
	}}
}

// ---- finding ----

// The lookup host function: of these ids, which are already items here?
// The provider is this plugin's OWN id, which is the namespace its source
// items are registered under, so what comes back is its own library.
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

func lookup(ids []string) map[string]ref {
	req, err := json.Marshal(lookupOp{Provider: pluginID, IDs: ids})
	if err != nil {
		return nil
	}
	ptr := mpAlloc(int32(len(req)))
	copy(live[ptr], req)
	var out lookupResult
	json.Unmarshal(unpack(hostLookup(ptr, int32(len(req)))), &out)
	return out.Found
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
	Kind   string            `json:"kind"`
	ItemID int64             `json:"itemId,omitempty"`
	PageID string            `json:"pageId,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// find is the composition, and it is the whole reason this plugin declares
// four families. A hit that is already in somebody's library opens the
// ITEM, so it arrives with its progress, its rating chip and its Play
// button. A hit that is not opens one of this plugin's own pages, which is
// somewhere to read about it and nothing more.
func find(q string) response {
	q = strings.TrimSpace(strings.ToLower(q))
	if len(q) < 3 {
		return response{}
	}
	matched := []show{}
	ids := []string{}
	for _, s := range shows {
		if strings.Contains(strings.ToLower(s.title), q) {
			matched = append(matched, s)
			ids = append(ids, s.id)
		}
	}
	if len(matched) == 0 {
		return response{} // null: the common answer, and it costs nobody anything
	}
	here := lookup(ids)
	rows := []tile{}
	for _, s := range matched {
		row := tile{Title: s.title, Art: art{Glyph: "film", Tint: "violet"}}
		if r, ok := here[s.id]; ok && r.ItemID != 0 {
			row.Subtitle = "In your library"
			row.Action = &link{Kind: "item", ItemID: r.ItemID}
		} else {
			row.Subtitle = "On the demo service"
			row.Action = &link{Kind: "page", PageID: "title", Params: map[string]string{"id": s.id}}
		}
		rows = append(rows, row)
	}
	return response{Data: map[string]any{"layout": "list", "results": rows}}
}

// ---- a page for what is not in the library ----

type block struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Rows []fact `json:"rows,omitempty"`
}

type fact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func page(raw json.RawMessage) response {
	var call struct {
		PageID string            `json:"pageId"`
		Params map[string]string `json:"params"`
	}
	json.Unmarshal(raw, &call)
	id := call.Params["id"]
	for _, s := range shows {
		if s.id != id {
			continue
		}
		return response{Data: map[string]any{
			"title": s.title,
			"blocks": []block{
				{Type: "text", Text: "This title is on the demo service. Connect your account and it appears in your own library, where it plays like anything else."},
				{Type: "factList", Rows: []fact{{Label: "Runtime", Value: strconv.Itoa(s.minutes) + " min"}}},
			},
		}}
	}
	return response{Error: "no such title"}
}

//go:wasmexport mp_handle
func mpHandle(ptr, n int32) int64 {
	var req request
	if err := json.Unmarshal(read(ptr, n), &req); err != nil {
		return respond(response{Error: "unreadable request"})
	}
	switch req.Op {
	case "link.begin":
		return respond(linkBegin())
	case "link.poll":
		return respond(linkPoll(req.Data))
	case "link.revoke":
		return respond(response{}) // nothing to tell a service that is a table
	case "source.sync":
		return respond(sync())
	case "source.resolve":
		return respond(resolve(req.Data))
	case "search.query":
		var q struct {
			Q string `json:"q"`
		}
		json.Unmarshal(req.Data, &q)
		return respond(find(q.Q))
	case "page.render":
		return respond(page(req.Data))
	}
	return respond(response{Error: "unsupported op " + req.Op})
}

func main() {}
