//go:build wasip1

// The D165 reference plugin for the EVENTS family: a Trakt scrobbler.
//
// It is the shape phase 3 was built for. Each person connects their own
// Trakt account through the device-code flow, and from then on what they
// watch here appears in their Trakt history, with pause and resume handled
// properly rather than a single write at the end.
//
// What it shows a plugin author:
//
//   - Per-profile linking. link.begin / link.poll / link.revoke, and the
//     opaque `state` blob the host hands in and stores back. The plugin
//     never keeps a token itself: the host holds it, per profile, and a
//     person disconnecting makes it disappear.
//   - Server-wide settings AND per-profile credentials in one plugin. The
//     owner supplies the Trakt application once; each person supplies
//     their own account.
//   - Refreshing an expiring token mid-event by returning new state.
//   - Reporting that the far end revoked an account, with `unlink`, so a
//     dead token stops being retried instead of failing forever.
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

//go:wasmimport mp config
func hostConfig() int64

//go:wasmimport mp http
func hostHTTP(ptr, n int32) int64

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

type errString string

func (e errString) Error() string { return string(e) }

// ---- Trakt ----

const apiBase = "https://api.trakt.tv"

// linkState is what this plugin asks the host to keep for one profile. It
// is opaque to the host, which is the point: the credentials belong to the
// person, live on their server, and never travel through the API.
type linkState struct {
	DeviceCode   string `json:"deviceCode,omitempty"` // only while a link is in progress
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	// ExpiresAt is a unix second. Trakt access tokens last three months,
	// so this is refreshed long before anyone notices.
	ExpiresAt int64 `json:"expiresAt,omitempty"`
	Username  string `json:"username,omitempty"`
}

// httpDo is one request through the host. The allowlist in the manifest
// means this can only ever reach api.trakt.tv, whatever this code does.
func httpDo(method, path string, body any, token string) (int, []byte, error) {
	headers := map[string]string{
		"Content-Type":      "application/json",
		"trakt-api-version": "2",
	}
	if id := strings.TrimSpace(settings()["clientId"]); id != "" {
		headers["trakt-api-key"] = id
	}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	payload := ""
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		payload = string(b)
	}
	req, err := json.Marshal(map[string]any{
		"method": method, "url": apiBase + path, "headers": headers, "body": payload,
	})
	if err != nil {
		return 0, nil, err
	}
	raw := unpack(hostHTTP(int32(uintptr(unsafe.Pointer(&req[0]))), int32(len(req))))
	var resp struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, nil, err
	}
	if resp.Error != "" {
		return 0, nil, errString(resp.Error)
	}
	return resp.Status, []byte(resp.Body), nil
}

// credentials returns the owner-supplied application, or an error phrased
// for whoever has to fix it.
func credentials() (string, string, error) {
	s := settings()
	id, secret := strings.TrimSpace(s["clientId"]), strings.TrimSpace(s["clientSecret"])
	if id == "" || secret == "" {
		return "", "", errString("Trakt is not set up yet: the server owner needs to add a Trakt client ID and secret in the plugin's settings.")
	}
	return id, secret, nil
}

// ---- linking ----

// linkBegin asks Trakt for a device code. This is the flow built for TVs
// and media centers: no redirect URL, no browser on the device, a short
// code typed on a phone.
func linkBegin() (map[string]any, error) {
	id, _, err := credentials()
	if err != nil {
		return nil, err
	}
	status, body, err := httpDo("POST", "/oauth/device/code", map[string]any{"client_id": id}, "")
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, errString("Trakt would not start the sign-in (HTTP " + strconv.Itoa(status) + "). Check the client ID in this plugin's settings.")
	}
	var out struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	state, _ := json.Marshal(linkState{DeviceCode: out.DeviceCode})
	url := out.VerificationURL
	if !strings.HasPrefix(url, "https://") {
		url = "https://trakt.tv/activate"
	}
	return map[string]any{
		"instructions": "Open the page below on your phone or computer, sign in to Trakt, and type this code.",
		"url":          url,
		"code":         out.UserCode,
		"expiresIn":    out.ExpiresIn,
		"interval":     out.Interval,
		"state":        json.RawMessage(state),
	}, nil
}

// linkPoll asks whether the person has finished. Trakt says so through
// status codes rather than a body, and each one means something different
// to the person waiting.
func linkPoll(state linkState) (map[string]any, error) {
	id, secret, err := credentials()
	if err != nil {
		return nil, err
	}
	if state.DeviceCode == "" {
		return nil, errString("that sign-in attempt has gone; start again.")
	}
	status, body, err := httpDo("POST", "/oauth/device/token", map[string]any{
		"code": state.DeviceCode, "client_id": id, "client_secret": secret,
	}, "")
	if err != nil {
		return nil, err
	}
	switch status {
	case 400:
		// The normal answer until they finish on the other device.
		return map[string]any{"pending": true}, nil
	case 404:
		return nil, errString("Trakt does not recognise that code any more; start again.")
	case 409:
		return nil, errString("that code was already used; start again.")
	case 410:
		return nil, errString("that code expired; start again.")
	case 418:
		return nil, errString("the sign-in was declined on Trakt.")
	case 429:
		// Polling too fast. Pending rather than an error: the host will
		// come back, and telling the person off would be pointless.
		return map[string]any{"pending": true}, nil
	case 200:
	default:
		return nil, errString("Trakt answered HTTP " + strconv.Itoa(status) + " while signing in.")
	}
	tok, err := parseToken(body)
	if err != nil {
		return nil, err
	}
	tok.Username = fetchUsername(tok.AccessToken)
	next, _ := json.Marshal(tok)
	account := tok.Username
	if account == "" {
		account = "Trakt"
	}
	return map[string]any{
		"linked": true, "account": account, "state": json.RawMessage(next),
	}, nil
}

// linkRevoke tells Trakt the token is finished with. The host forgets the
// link whatever happens here, so a failure only means Trakt keeps a dead
// token until it expires on its own.
func linkRevoke(state linkState) {
	id, secret, err := credentials()
	if err != nil || state.AccessToken == "" {
		return
	}
	httpDo("POST", "/oauth/revoke", map[string]any{
		"token": state.AccessToken, "client_id": id, "client_secret": secret,
	}, "")
}

func parseToken(body []byte) (linkState, error) {
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		CreatedAt    int64  `json:"created_at"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return linkState{}, err
	}
	if out.AccessToken == "" {
		return linkState{}, errString("Trakt returned no access token.")
	}
	return linkState{
		AccessToken: out.AccessToken, RefreshToken: out.RefreshToken,
		ExpiresAt: out.CreatedAt + out.ExpiresIn,
	}, nil
}

func fetchUsername(token string) string {
	status, body, err := httpDo("GET", "/users/settings", nil, token)
	if err != nil || status != 200 {
		return ""
	}
	var out struct {
		User struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"user"`
	}
	if json.Unmarshal(body, &out) != nil {
		return ""
	}
	if out.User.Username != "" {
		return out.User.Username
	}
	return out.User.Name
}

// refreshIfStale renews a token that is close to expiring. Returning the
// new state from the event op is how it gets persisted, so a refresh costs
// one extra call and nothing else.
func refreshIfStale(state linkState, now int64) (linkState, bool) {
	// A day's margin: tokens last months, so this never races anything.
	if state.RefreshToken == "" || state.ExpiresAt == 0 || state.ExpiresAt-now > 86400 {
		return state, false
	}
	id, secret, err := credentials()
	if err != nil {
		return state, false
	}
	status, body, err := httpDo("POST", "/oauth/token", map[string]any{
		"refresh_token": state.RefreshToken, "client_id": id, "client_secret": secret,
		"redirect_uri": "urn:ietf:wg:oauth:2.0:oob", "grant_type": "refresh_token",
	}, "")
	if err != nil || status != 200 {
		return state, false
	}
	next, err := parseToken(body)
	if err != nil {
		return state, false
	}
	next.Username = state.Username
	return next, true
}

// ---- scrobbling ----

// mediaBody turns one item into what Trakt wants to be told about.
//
// A TMDB id is exact, so it is used when the item has one. Anything else
// (a title matched by a plugin like AniList, or a hand-entered one) falls
// back to title and year, which Trakt matches well enough for a scrobble
// and is far better than refusing to scrobble at all.
func mediaBody(item map[string]any, progress float64) map[string]any {
	kind, _ := item["kind"].(string)
	title, _ := item["title"].(string)
	year := intOf(item["year"])
	tmdb := 0
	if prov, _ := item["metaProvider"].(string); prov == "tmdb" {
		if id, _ := item["metaId"].(string); id != "" {
			tmdb, _ = strconv.Atoi(id)
		}
	}
	body := map[string]any{"progress": progress}
	if kind == "episode" {
		show := map[string]any{}
		if s, _ := item["series"].(string); s != "" {
			show["title"] = s
		}
		if tmdb > 0 {
			// An episode's metaId is its SERIES id on this server, which is
			// exactly what Trakt wants in the show object.
			show["ids"] = map[string]any{"tmdb": tmdb}
		}
		body["show"] = show
		body["episode"] = map[string]any{
			"season": intOf(item["season"]), "number": intOf(item["episode"]),
		}
		return body
	}
	movie := map[string]any{}
	if title != "" {
		movie["title"] = title
	}
	if year > 0 {
		movie["year"] = year
	}
	if tmdb > 0 {
		movie["ids"] = map[string]any{"tmdb": tmdb}
	}
	body["movie"] = movie
	return body
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// scrobbleFor maps our events onto Trakt's three verbs.
//
// started/resumed → start, paused → pause, stopped/finished → stop. Trakt
// decides from the progress whether a stop is a completed watch (past ~80%)
// or a pause, which is why the progress on a stop matters more than the
// event name does.
func scrobbleFor(event string) string {
	switch event {
	case "playback.started", "playback.resumed":
		return "start"
	case "playback.paused":
		return "pause"
	case "playback.stopped", "playback.finished":
		return "stop"
	}
	return ""
}

// handleEvent is the whole scrobbler.
func handleEvent(in map[string]any) (map[string]any, error) {
	event, _ := in["event"].(string)
	verb := scrobbleFor(event)
	if verb == "" {
		return map[string]any{}, nil
	}
	var state linkState
	if raw, ok := in["state"]; ok && raw != nil {
		b, _ := json.Marshal(raw)
		json.Unmarshal(b, &state)
	}
	if state.AccessToken == "" {
		// Not connected. This should not happen (the host only delivers to
		// linked profiles) but saying so beats a confusing 401.
		return nil, errString("this profile is not connected to Trakt.")
	}
	at := int64(0)
	if v, ok := in["at"].(float64); ok {
		at = int64(v)
	}
	state, refreshed := refreshIfStale(state, at)

	item, _ := in["item"].(map[string]any)
	session, _ := in["session"].(map[string]any)
	progress, _ := session["progress"].(float64)
	// A finish is a finish even if the file's duration disagreed with the
	// player: report 100 so Trakt records a watch rather than a near-miss.
	if event == "playback.finished" {
		progress = 100
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	// Trakt refuses a start below 1%, and a scrobble at 0 is noise anyway.
	if verb == "start" && progress < 1 {
		progress = 1
	}

	status, body, err := httpDo("POST", "/scrobble/"+verb, mediaBody(item, progress), state.AccessToken)
	out := map[string]any{}
	if refreshed {
		next, _ := json.Marshal(state)
		out["state"] = json.RawMessage(next)
	}
	if err != nil {
		return nil, err
	}
	switch {
	case status == 401:
		// The account was revoked at Trakt's end. Say so, and the host
		// forgets the link rather than retrying a dead token forever.
		out["unlink"] = true
		out["note"] = "Trakt no longer accepts this account, so it has been disconnected."
		return out, nil
	case status == 404:
		// Trakt does not know this title. Not an error worth failing over:
		// one unmatched film should not make the plugin look broken.
		out["note"] = "Trakt could not find a match for this title, so it was not scrobbled."
		return out, nil
	case status == 409:
		// Already scrobbled recently. Trakt's own de-duplication.
		return out, nil
	case status >= 200 && status < 300:
		return out, nil
	}
	return nil, errString("Trakt answered HTTP " + strconv.Itoa(status) + " for " + verb + ": " + clip(string(body), 200))
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// ---- dispatch ----

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
	in := map[string]any{}
	json.Unmarshal(call.Data, &in)

	var state linkState
	if raw, ok := in["state"]; ok && raw != nil {
		b, _ := json.Marshal(raw)
		json.Unmarshal(b, &state)
	}

	switch call.Op {
	case "link.begin":
		out, err := linkBegin()
		if err != nil {
			return fail(err.Error())
		}
		return ok(out)

	case "link.poll":
		out, err := linkPoll(state)
		if err != nil {
			return fail(err.Error())
		}
		return ok(out)

	case "link.revoke":
		linkRevoke(state)
		return ok(map[string]any{})

	case "event.deliver":
		out, err := handleEvent(in)
		if err != nil {
			return fail(err.Error())
		}
		return ok(out)
	}
	return fail("this plugin does not implement " + call.Op)
}

func ok(data any) int64 {
	b, err := json.Marshal(map[string]any{"data": data})
	if err != nil {
		return fail("could not encode the reply")
	}
	return packed(b)
}

func fail(msg string) int64 {
	b, _ := json.Marshal(map[string]any{"error": msg})
	return packed(b)
}

func main() {}
