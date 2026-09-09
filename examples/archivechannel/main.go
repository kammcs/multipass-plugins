//go:build wasip1

// The Internet Archive as a CHANNEL (D178), not a library.
//
// examples/plugins/archive is the same service as one library behind one
// collection slug. This is the same service arranged: eight libraries the
// plugin chooses, a page it ships, and a curated core it vouches for.
//
// The three things worth copying out of here:
//
//   - CURATION IS AN ALLOWLIST. The archive has no content rating and no
//     adult flag, its newest uploads are bootleg rips with the upload year
//     written into `year`, and its most-watched-this-week is pornography.
//     There is nothing to filter against, so this plugin carries a table
//     of identifiers it vouches for and the queries sit behind it as the
//     deep catalog. docs/ARCHIVE-CHANNEL.md section 3 has the measurements.
//   - THE CURATED ROWS CARRY TMDB IDS. `meta` on a synced item is written
//     to items.meta_provider/meta_id, and a channel library's provider is
//     `tmdb` by default, so the host enriches those titles BY ID: real
//     posters, backdrops, cast and certifications, with no search. An item
//     with no `meta` is stamped `none` and correctly skipped. That is the
//     whole difference between this and a grid of 180x124 thumbnails.
//   - ONE ARCHIVE ITEM IS NOT ONE TITLE. In classic_tv an item is a whole
//     series (GreenAcresCompleteSeries holds 169 playable files), a
//     miniseries, one episode, or nothing playable at all. That library is
//     show-shaped and reads each item's file list at sync.
//
// Two public endpoints, no key and no account:
//
//	advancedsearch.php   enumerate a collection
//	metadata/<id>        one title's files
//
// Build:
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
//	multipass plugins pack . -out archivechannel.mpp
//
// Without -buildmode=c-shared the module is a COMMAND, not a reactor, and
// the host cannot call into it.
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

func unpack(v int64) []byte {
	if v == 0 {
		return nil
	}
	return read(int32(v>>32), int32(v))
}

//go:wasmimport mp http
func hostHTTP(ptr, n int32) int64

//go:wasmimport mp lookup
func hostLookup(ptr, n int32) int64

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

type errString string

func (e errString) Error() string { return string(e) }

// call hands a JSON request to a host function and returns its answer.
func call(fn func(ptr, n int32) int64, req []byte) []byte {
	if len(req) == 0 {
		return nil
	}
	ptr := mpAlloc(int32(len(req)))
	copy(live[ptr], req)
	return unpack(fn(ptr, int32(len(req))))
}

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
	var resp struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(call(hostHTTP, req), &resp); err != nil {
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

// ---- the wire, exactly as the host speaks it ----

// syncLibrary is which library the host is asking about. `key` is the
// channel addition: it is the key this plugin's manifest gave the library,
// so a plugin holding several catalogs knows which one it is enumerating.
type syncLibrary struct {
	Key    string            `json:"key,omitempty"`
	Shape  string            `json:"shape"`
	Config map[string]string `json:"config,omitempty"`
}

type syncRequest struct {
	Library syncLibrary `json:"library"`
	Cursor  string      `json:"cursor,omitempty"`
}

type syncPage struct {
	Series   []srcSeries `json:"series,omitempty"`
	Items    []srcItem   `json:"items,omitempty"`
	Next     string      `json:"next,omitempty"`
	Complete bool        `json:"complete,omitempty"`
}

// metaRef names a match in a metadata service, so a title arrives with
// posters and cast for free. It is only ever set from the curated table:
// searching a metadata service for the title of an archive upload finds
// something, and something is worse than nothing.
type metaRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

type srcSeries struct {
	ExternalID string   `json:"externalId"`
	Title      string   `json:"title"`
	Year       int      `json:"year,omitempty"`
	Overview   string   `json:"overview,omitempty"`
	Poster     string   `json:"poster,omitempty"`
	Meta       *metaRef `json:"meta,omitempty"`
}

type srcItem struct {
	ExternalID string   `json:"externalId"`
	Title      string   `json:"title"`
	Year       int      `json:"year,omitempty"`
	Overview   string   `json:"overview,omitempty"`
	RuntimeMin int      `json:"runtimeMin,omitempty"`
	Poster     string   `json:"poster,omitempty"`
	Series     string   `json:"series,omitempty"`
	Season     int      `json:"season,omitempty"`
	Episode    int      `json:"episode,omitempty"`
	Meta       *metaRef `json:"meta,omitempty"`
}

type resolveRequest struct {
	Library syncLibrary `json:"library"`
	Item    struct {
		ExternalID string `json:"externalId"`
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

type resolved struct {
	URL       string `json:"url"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
	Hint      string `json:"hint,omitempty"`
}

// renderRequest is the channel family's one op. `slot` is the id of the
// slot block in the layout; `locale` is the viewer's language.
type renderRequest struct {
	Slot   string `json:"slot"`
	Locale string `json:"locale,omitempty"`
}

// lookupOp asks the host which of THIS plugin's external ids are already
// items here, in this plugin's own namespace only and only about libraries
// the person asking can see. At most maxLookupIDs per call.
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

// pluginID is this plugin's own id, which is the provider namespace the
// host answers a lookup in.
const pluginID = "theater.multipass.archivechannel"

// maxLookupIDs is the host's cap on one lookup call.
const maxLookupIDs = 100

// ---- one item's file list ----
//
// Both the show sync and the resolver read this, which is why it lives
// here rather than in either of them. Two people writing the same reader
// from one sentence is how the last stage got a contract mismatch.

type iaFile struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Title  string `json:"title,omitempty"`
	Length string `json:"length,omitempty"`
}

type iaMeta struct {
	Files []iaFile `json:"files"`
}

// itemMeta reads one item's files. This is the expensive call: one round
// trip per title, which is why the movie libraries do it at Play and only
// the show library does it at sync.
func itemMeta(id string) (iaMeta, error) {
	var m iaMeta
	err := get("https://archive.org/metadata/"+pathEscape(id), &m)
	return m, err
}

// playableFiles is the copies of an item this server should stream, in the
// order the item lists them, which for a series item is the order the
// episodes are in. An item with none is one to skip rather than to file
// and fail at Play.
//
// The prefix match is not sloppiness, it is the correction a measurement
// forced. The search index treats `format:"h.264"` as a TOKEN match, so it
// returns items whose derivatives are actually filed as `h.264 IA`, and an
// exact string compare here threw those away after the query had already
// accepted them. `get-smart` is the case that found it: the index returns
// it, its file list holds **137** `h.264 IA` episodes, and an exact match
// reported zero and skipped a whole series that plays.
//
// MPEG4 is the second tier and only when there is no h.264 at all, so a
// series never gets each episode twice. `theloneranger_201705` (16 files)
// and `Bonanza_pd` (30) are the items that need it: they carry no h.264
// derivative of any spelling, and the server can remux what they do have.
func (m iaMeta) playableFiles() []iaFile {
	h264, mpeg4 := []iaFile{}, []iaFile{}
	for _, f := range m.Files {
		if f.Name == "" {
			continue
		}
		switch {
		case strings.HasPrefix(strings.ToLower(f.Format), "h.264"):
			h264 = append(h264, f)
		case strings.Contains(strings.ToLower(f.Format), "mpeg4") && videoExt(f.Name):
			mpeg4 = append(mpeg4, f)
		}
	}
	if len(h264) > 0 {
		return h264
	}
	return mpeg4
}

// videoExt guards the second tier. An item's `MPEG4` rows are usually its
// video, but the format string is not a promise about the file name, and a
// row that is not a video is an episode that cannot play.
func videoExt(name string) bool {
	switch ext(name) {
	case ".mp4", ".m4v", ".mpeg", ".mpg", ".mkv", ".ogv", ".webm", ".avi":
		return true
	}
	return false
}

// downloadURL is the address the archive publishes for one file. It
// redirects to whichever machine holds the bytes today; the server settles
// that itself and holds every hop to https and to a destination outside
// the house.
func downloadURL(ia, file string) string {
	return "https://archive.org/download/" + pathEscape(ia) + "/" + escapePath(file)
}

// episodeID packs an item identifier and one of its files into the id one
// episode is known by, and splitEpisodeID reverses it.
//
// The separator is `#`, which cannot appear in an archive identifier. An
// episode's identity is therefore only as stable as the file NAME, and
// uploaders do rename files: a rename presents as a new episode, and the
// old one goes on the next complete pass. That is the honest cost of a
// service where one item is a hundred episodes.
func episodeID(ia, file string) string { return ia + "#" + file }

func splitEpisodeID(id string) (string, string) {
	if i := strings.Index(id, "#"); i >= 0 {
		return id[:i], id[i+1:]
	}
	return id, ""
}

// ---- shared text helpers ----

// pathEscape escapes one path segment. The archive's identifiers are
// ASCII slugs, but a file name is whatever somebody typed in 1998.
func pathEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
			continue
		}
		const hex = "0123456789ABCDEF"
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

// escapePath escapes each segment of a file name, because an item's files
// live in folders often enough (a season per folder, in the show library)
// and a slash is a separator, not a character to escape.
func escapePath(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = pathEscape(p)
	}
	return strings.Join(parts, "/")
}

func ext(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.ToLower(name[i:])
	}
	return ""
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

// maxOverview keeps a description somebody typed as a shot list in 1994
// from arriving as forty kilobytes of prose. The host clips too; this
// keeps it off the wire.
const maxOverview = 600

// sensibleYear rejects the archive's commonest metadata lie. A recent
// upload often carries the UPLOAD year in `year`, so a 1982 film reads
// 2026. Anything outside the range of cinema is no year at all.
func sensibleYear(n int) int {
	if n > 1870 && n < 2100 {
		return n
	}
	return 0
}

// firstNumber reads the leading run of digits, for fields that arrive as
// "1953" and as "1953-01-01" and occasionally as "circa 1953".
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
	case "plugin.setup":
		// D179. The manifest declares `setup: true`, which is what makes
		// the host call this after an install, an upgrade or a re-enable.
		// Declaring it and not answering here would be `unsupported op`,
		// which the host treats as nothing to do rather than as a broken
		// install, so the failure mode of forgetting this line is quiet.
		return respond(pluginSetup(req.Data))
	}
	return respond(response{Error: "unsupported op " + req.Op})
}

func main() {}
