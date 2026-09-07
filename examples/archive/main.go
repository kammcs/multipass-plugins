//go:build wasip1

// The D176 source family against a REAL service: archive.org.
//
// feeddemo shows the mechanics with a table it carries. This one is the
// same family pointed at something that is actually up, with all the
// awkwardness that comes with it: paging a result set of ten thousand,
// descriptions written by hand thirty years ago, a search index whose
// fields are sometimes a string and sometimes a list, and a download
// address that redirects to whichever machine holds the bytes today.
//
// Two public endpoints, no key and no account:
//
//	advancedsearch.php   enumerate a collection
//	metadata/<id>        one title's files
//
// The split between them is the shape worth copying. A sync is cheap and
// paginated, so it asks only the search index. Picking a file needs the
// item's file list, which is one request per title, so it happens at Play
// instead: 2,000 requests a sync would be rude to a service that gives its
// catalog away for free.
//
// Three things this shows that a demo cannot:
//
//   - `externalId` is the archive identifier. It was chosen by somebody
//     else, it is stable for the life of the item, and it is exactly what
//     the phase means by identity: re-sync and the row you made last time
//     is updated, so progress, watched state and playlists survive.
//   - `complete: true` is claimed ONLY when the enumeration really reached
//     the end. This plugin stops after twenty pages, so a large collection
//     is truncated, and a truncated pass has not listed everything and must
//     not be allowed to delete. The library then only grows, which is the
//     right way round: a shelf with a stale title on it is better than a
//     shelf somebody's film disappeared from.
//   - the resolver returns `archive.org/download/...`, which is the address
//     the archive publishes, and it redirects to whichever file server has
//     the bytes (dn790002.ca.archive.org one day, ia801604.us.archive.org
//     the next). The manifest grants archive.org and nothing else. The
//     server settles that redirect itself and holds every hop to https and
//     to a destination outside the house, but it does NOT hold the hops to
//     the grant, because where a service keeps its bytes is not something
//     an owner can usefully approve. PLUGIN-LIBRARIES.md section 18 is the
//     ruling.
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
//	multipass plugins pack . -out archive.mpp
package main

import (
	"encoding/json"
	"net/url"
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

//go:wasmimport mp http
func hostHTTP(ptr, n int32) int64

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

type errString string

func (e errString) Error() string { return string(e) }

// get fetches one address through the host, which enforces the manifest's
// allowlist, so this can only ever reach archive.org.
func get(addr string, out any) error {
	req, err := json.Marshal(map[string]any{
		"method":  "GET",
		"url":     addr,
		"headers": map[string]string{"Accept": "application/json"},
	})
	if err != nil {
		return err
	}
	raw := unpack(hostHTTP(int32(uintptr(unsafe.Pointer(&req[0]))), int32(len(req))))
	var resp struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errString(resp.Error)
	}
	if resp.Status != 200 {
		return errString("archive.org answered HTTP " + strconv.Itoa(resp.Status))
	}
	return json.Unmarshal([]byte(resp.Body), out)
}

// ---- the contract ----

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
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	Year       int    `json:"year,omitempty"`
	Overview   string `json:"overview,omitempty"`
	RuntimeMin int    `json:"runtimeMin,omitempty"`
	Poster     string `json:"poster,omitempty"`
}

type resolveRequest struct {
	Item struct {
		ExternalID string `json:"externalId"`
		Title      string `json:"title,omitempty"`
	} `json:"item"`
}

type resolved struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// ---- reading a search index that answers in whatever shape it likes ----

// loose is a field the index returns as a string, as a number, or as a
// list of either, depending on the item. Every real service has a few, and
// a plugin that assumes one shape drops rows for no reason a user could
// guess.
type loose struct{ s string }

func (l *loose) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil // a field we cannot read is a field we do without
	}
	l.s = flatten(v)
	return nil
}

func flatten(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := []string{}
		for _, e := range t {
			if s := flatten(e); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

type searchDoc struct {
	Identifier  string `json:"identifier"`
	Title       loose  `json:"title"`
	Year        loose  `json:"year"`
	Description loose  `json:"description"`
	Runtime     loose  `json:"runtime"`
}

type searchResult struct {
	Response struct {
		NumFound int         `json:"numFound"`
		Start    int         `json:"start"`
		Docs     []searchDoc `json:"docs"`
	} `json:"response"`
}

// ---- the sync ----

const (
	// rows is one page of the search index, and it is also one page of the
	// answer: the host caps a page at 200 rows, so the two line up.
	rows = 100
	// maxPages is where this plugin stops. Twenty pages is a library, not a
	// database, and a household's first plugin library should be something
	// somebody can scroll. Stopping here is also why `complete` below is
	// conditional: a truncated pass has not listed everything.
	maxPages = 20
	// maxOverview keeps a description that somebody typed as a shot list in
	// 1994 from arriving as forty kilobytes of prose. The host clips too;
	// this keeps it off the wire.
	maxOverview = 600
)

func collection(req syncRequest) string {
	c := strings.TrimSpace(req.Library.Config["collection"])
	if c == "" {
		return "prelinger"
	}
	return c
}

func searchURL(coll string, page int) string {
	// format:"h.264" is the archive's own MP4 derivative. Asking the index
	// for it means the catalog holds only titles that will actually play,
	// rather than filing a row now and failing at Play.
	q := `collection:"` + coll + `" AND mediatype:movies AND format:"h.264"`
	v := url.Values{}
	v.Set("q", q)
	v.Set("rows", strconv.Itoa(rows))
	v.Set("page", strconv.Itoa(page))
	v.Set("output", "json")
	for _, f := range []string{"identifier", "title", "year", "description", "runtime"} {
		v.Add("fl[]", f)
	}
	// Most-downloaded first, so a truncated collection is truncated at the
	// end nobody misses. The identifier breaks ties, so paging is stable
	// within one pass.
	v.Add("sort[]", "downloads desc")
	v.Add("sort[]", "identifier asc")
	return "https://archive.org/advancedsearch.php?" + v.Encode()
}

func sync(raw json.RawMessage) response {
	var req syncRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable sync request"}
		}
	}
	page := 1
	if req.Cursor != "" {
		n, err := strconv.Atoi(req.Cursor)
		if err != nil || n < 1 {
			return response{Error: "unreadable cursor"}
		}
		page = n
	}
	var res searchResult
	if err := get(searchURL(collection(req), page), &res); err != nil {
		// An honest failure. The host records it, shows it in the hub, and
		// changes nothing in the library, which is the whole reason a sync
		// that fails is different from a sync that returns less.
		return response{Error: "could not read the collection: " + err.Error()}
	}
	docs := res.Response.Docs
	if len(docs) == 0 && page == 1 {
		return response{Error: "that collection has no films with a playable copy"}
	}
	items := make([]srcItem, 0, len(docs))
	for _, d := range docs {
		if it, ok := item(d); ok {
			items = append(items, it)
		}
	}
	// The end of the result set, or the end of this plugin's patience.
	// Only the first of those has listed everything, and only the first may
	// authorize the host to delete what it did not see.
	seen := res.Response.Start + len(docs)
	done := len(docs) < rows || seen >= res.Response.NumFound
	out := syncPage{Items: items, Complete: done}
	if !done && page < maxPages {
		out.Next = strconv.Itoa(page + 1)
	}
	return response{Data: out}
}

func item(d searchDoc) (srcItem, bool) {
	id := strings.TrimSpace(d.Identifier)
	title := strings.TrimSpace(d.Title.s)
	if id == "" || title == "" {
		return srcItem{}, false
	}
	it := srcItem{
		ExternalID: id,
		Title:      title,
		Overview:   clip(strings.TrimSpace(strip(d.Description.s)), maxOverview),
		RuntimeMin: minutes(d.Runtime.s),
		// The archive renders a thumbnail for every item. The server
		// fetches and caches it, so a viewer's device never connects to
		// archive.org for a picture either. This is the CDN-served one and
		// it names no file, which is fine: the cache reads the format off
		// the response when the address does not say. The thumbnail beside
		// the item's own files (download/<id>/__ia_thumb.jpg) is the same
		// picture through a redirect, and about three times slower, which
		// over a whole collection is the difference between minutes.
		Poster: "https://archive.org/services/img/" + url.PathEscape(id),
	}
	if y, err := strconv.Atoi(firstNumber(d.Year.s)); err == nil && y > 1870 && y < 2200 {
		it.Year = y
	}
	return it, true
}

// strip removes the HTML a description may be written in. Descriptions on
// the archive are decades of hand-typed markup; a title tag in a browse
// card is not markup, it is noise.
func strip(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>' && depth > 0:
			depth--
			b.WriteByte(' ')
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + "..."
}

// minutes reads the archive's runtime, which is "28:19" or "1:04:12" or
// occasionally something else entirely. Anything it cannot read is no
// runtime at all, which is fine: a declared duration is a hint, and the
// probe at first play is what the server acts on.
func minutes(s string) int {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0
	}
	secs := 0
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return 0
		}
		secs = secs*60 + n
	}
	return secs / 60
}

func firstNumber(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			break
		}
	}
	return b.String()
}

// ---- the resolver ----

type metadata struct {
	Files []struct {
		Name   string `json:"name"`
		Format string `json:"format"`
		Source string `json:"source"`
	} `json:"files"`
}

// streamTTL is how long the address is reused. The archive's are not
// signed and do not expire, but a file list can change, so a day is the
// point at which asking again is cheaper than being wrong.
const streamTTL = 24 * 60 * 60

// playable ranks the files of one item. The archive derives an MP4 for
// nearly everything it holds, and that derivative is the one to take: the
// original is as often a 380 MB MPEG-2 or a Cinepak AVI, and while the
// server can remux either, it should not have to.
var playable = []struct {
	format string
	ext    string
}{
	{"h.264", ".mp4"},
	{"", ".mp4"},
	{"", ".m4v"},
	{"", ".webm"},
	{"", ".ogv"},
	{"", ".mkv"},
	{"", ".mpeg"},
	{"", ".mpg"},
	{"", ".avi"},
}

func resolve(raw json.RawMessage) response {
	var req resolveRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable resolve request"}
		}
	}
	id := strings.TrimSpace(req.Item.ExternalID)
	if id == "" {
		return response{Error: "no title to resolve"}
	}
	var meta metadata
	if err := get("https://archive.org/metadata/"+url.PathEscape(id), &meta); err != nil {
		return response{Error: "could not read that title: " + err.Error()}
	}
	name := pick(meta)
	if name == "" {
		return response{Error: "that title has no copy this server can play"}
	}
	// The address the archive publishes, on the host this plugin asked
	// permission for. It redirects to whichever file server holds the
	// bytes; the server settles that itself before it fetches.
	return response{Data: resolved{
		URL:       "https://archive.org/download/" + url.PathEscape(id) + "/" + escapePath(name),
		ExpiresAt: time.Now().Unix() + streamTTL,
		Hint:      strings.TrimPrefix(ext(name), "."),
	}}
}

func pick(meta metadata) string {
	for _, want := range playable {
		for _, f := range meta.Files {
			if f.Name == "" || ext(f.Name) != want.ext {
				continue
			}
			if want.format != "" && !strings.EqualFold(f.Format, want.format) {
				continue
			}
			return f.Name
		}
	}
	return ""
}

func ext(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.ToLower(name[i:])
	}
	return ""
}

// escapePath escapes each segment of a file name, because an item's files
// live in folders often enough and a slash is a separator, not a space.
func escapePath(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
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
	}
	return respond(response{Error: "unsupported op " + req.Op})
}

func main() {}
