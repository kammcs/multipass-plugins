//go:build wasip1

// The D165 reference plugin for a REAL matching service: AniList.
//
// It is the shape phase 1 was built for. An anime library on a server with
// no TMDB account at all can match entirely through this: AniList knows
// series and films that general movie databases handle badly, and its API
// is public, free and needs no key.
//
// What it shows a plugin author: the http capability (one allowlisted
// host, declared in the manifest and visible to the owner at install),
// settings that change behavior, and the matching family's ops.
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

//go:wasmimport mp storage
func hostStorage(ptr, n int32) int64

// cacheGet/cacheSet use the storage grant. Caching matters here for a
// concrete reason: the host asks for episodes ONE AT A TIME, so a 26
// episode season would be 26 API calls against a service that rate-limits.
// One fetch fills the whole season.
func cacheGet(key string) string {
	req, err := json.Marshal(map[string]any{"op": "get", "key": key})
	if err != nil {
		return ""
	}
	var out struct {
		Value string `json:"value"`
	}
	json.Unmarshal(unpack(hostStorage(int32(uintptr(unsafe.Pointer(&req[0]))), int32(len(req)))), &out)
	return out.Value
}

func cacheSet(key, value string) {
	req, err := json.Marshal(map[string]any{"op": "set", "key": key, "value": value})
	if err != nil {
		return
	}
	hostStorage(int32(uintptr(unsafe.Pointer(&req[0]))), int32(len(req)))
}

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

// post sends a GraphQL query through the host. The host enforces the
// manifest's allowlist, so this can only ever reach graphql.anilist.co.
func post(query string, vars map[string]any) (map[string]any, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return nil, err
	}
	req, err := json.Marshal(map[string]any{
		"method":  "POST",
		"url":     "https://graphql.anilist.co",
		"headers": map[string]string{"Content-Type": "application/json", "Accept": "application/json"},
		"body":    string(body),
	})
	if err != nil {
		return nil, err
	}
	raw := unpack(hostHTTP(int32(uintptr(unsafe.Pointer(&req[0]))), int32(len(req))))
	var resp struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errString(resp.Error)
	}
	if resp.Status != 200 {
		return nil, errString("AniList answered HTTP " + strconv.Itoa(resp.Status))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resp.Body), &out); err != nil {
		return nil, err
	}
	return out, nil
}

type errString string

func (e errString) Error() string { return string(e) }

// ---- AniList ----

const mediaFields = `
  id
  title { romaji english native }
  description(asHtml: false)
  averageScore
  duration
  episodes
  genres
  seasonYear
  startDate { year month day }
  coverImage { extraLarge large }
  bannerImage
  studios(isMain: true) { nodes { id name } }
  characters(sort: [ROLE, RELEVANCE], perPage: 20) {
    edges { role node { id name { full } image { large } } voiceActors(language: JAPANESE) { id name { full } image { large } } }
  }
  staff(sort: [RELEVANCE], perPage: 25) { edges { role node { id name { full } image { large } } } }
`

func searchQuery() string {
	return `query ($search: String, $type: MediaType, $year: Int, $adult: Boolean) {
      Page(perPage: 10) {
        media(search: $search, type: $type, seasonYear: $year, isAdult: $adult, sort: [SEARCH_MATCH]) {` + mediaFields + `}
      }
    }`
}

func byIDQuery() string {
	return `query ($id: Int) { Media(id: $id) {` + mediaFields + `} }`
}

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func num(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func obj(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func arr(m map[string]any, key string) []any {
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

// stripHTML flattens AniList's light markup, which arrives even with
// asHtml:false (it keeps <br> and <i>).
func stripHTML(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '<':
			depth++
		case r == '>':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(strings.ReplaceAll(b.String(), "\n\n\n", "\n\n"))
}

type meta struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Overview    string   `json:"overview,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Rating      float64  `json:"rating,omitempty"`
	RuntimeMin  int      `json:"runtimeMin,omitempty"`
	Year        int      `json:"year,omitempty"`
	ReleaseDate string   `json:"releaseDate,omitempty"`
	Poster      string   `json:"poster,omitempty"`
	Backdrop    string   `json:"backdrop,omitempty"`
	Cast        []person `json:"cast,omitempty"`
	Crew        []crew   `json:"crew,omitempty"`
	Studios     []studio `json:"studios,omitempty"`
}

type person struct {
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
	Photo     string `json:"photo,omitempty"`
	PersonID  int64  `json:"personId,omitempty"`
}

type crew struct {
	PersonID int64  `json:"personId,omitempty"`
	Name     string `json:"name"`
	Job      string `json:"job"`
	Photo    string `json:"photo,omitempty"`
}

type studio struct {
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name"`
}

// pickTitle honors the owner's title-language setting, falling back through
// the other forms rather than returning an empty title (which the host
// treats as a non-match).
func pickTitle(t map[string]any, pref string) string {
	order := []string{"romaji", "english", "native"}
	switch pref {
	case "english":
		order = []string{"english", "romaji", "native"}
	case "native":
		order = []string{"native", "romaji", "english"}
	}
	for _, k := range order {
		if v := str(t, k); v != "" {
			return v
		}
	}
	return ""
}

// toMeta maps one AniList Media node onto the host's wire shape.
//
// Note what is NOT set for a series: RuntimeMin. AniList's `duration` is
// per EPISODE, and the host cross-checks a searched match against the
// file's real duration, so reporting an episode length against a whole
// series file would make the host reject its own correct match.
func toMeta(node map[string]any, cfg map[string]string, isSeries bool) *meta {
	if node == nil {
		return nil
	}
	title := pickTitle(obj(node, "title"), cfg["titleLanguage"])
	if title == "" {
		return nil
	}
	m := &meta{
		ID:       strconv.Itoa(int(num(node, "id"))),
		Title:    title,
		Overview: stripHTML(str(node, "description")),
		Year:     int(num(node, "seasonYear")),
		Rating:   num(node, "averageScore") / 10, // AniList scores out of 100
	}
	if !isSeries {
		m.RuntimeMin = int(num(node, "duration"))
	}
	for _, g := range arr(node, "genres") {
		if s, ok := g.(string); ok {
			m.Genres = append(m.Genres, s)
		}
	}
	if d := obj(node, "startDate"); d != nil && num(d, "year") > 0 {
		m.ReleaseDate = pad(int(num(d, "year")), 4) + "-" + pad(int(num(d, "month")), 2) + "-" + pad(int(num(d, "day")), 2)
	}
	if c := obj(node, "coverImage"); c != nil {
		if m.Poster = str(c, "extraLarge"); m.Poster == "" {
			m.Poster = str(c, "large")
		}
	}
	m.Backdrop = str(node, "bannerImage")
	if s := obj(node, "studios"); s != nil {
		for _, n := range arr(s, "nodes") {
			if sm, ok := n.(map[string]any); ok {
				m.Studios = append(m.Studios, studio{ID: int64(num(sm, "id")), Name: str(sm, "name")})
			}
		}
	}
	// Cast: AniList models characters and their voice actors separately.
	// The performer is the voice actor, playing the character.
	if ch := obj(node, "characters"); ch != nil {
		for _, e := range arr(ch, "edges") {
			edge, ok := e.(map[string]any)
			if !ok {
				continue
			}
			charName := ""
			if cn := obj(obj(edge, "node"), "name"); cn != nil {
				charName = str(cn, "full")
			}
			vas := arr(edge, "voiceActors")
			if len(vas) == 0 {
				continue
			}
			va, ok := vas[0].(map[string]any)
			if !ok {
				continue
			}
			name := ""
			if n := obj(va, "name"); n != nil {
				name = str(n, "full")
			}
			if name == "" {
				continue
			}
			m.Cast = append(m.Cast, person{
				Name: name, Character: charName, PersonID: int64(num(va, "id")),
				Photo: str(obj(va, "image"), "large"),
			})
		}
	}
	if st := obj(node, "staff"); st != nil {
		for _, e := range arr(st, "edges") {
			edge, ok := e.(map[string]any)
			if !ok {
				continue
			}
			// AniList's staff list mixes the production crew with every
			// localization credit, and a loose match picks the wrong people:
			// "Cowboy Bebop" answers with three dub ADR directors before it
			// mentions Shinichiro Watanabe. So the roles are an exact
			// allowlist, not a substring test.
			job := crewJob(str(edge, "role"))
			if job == "" {
				continue
			}
			n := obj(edge, "node")
			name := ""
			if nn := obj(n, "name"); nn != nil {
				name = str(nn, "full")
			}
			if name == "" {
				continue
			}
			m.Crew = append(m.Crew, crew{
				PersonID: int64(num(n, "id")), Name: name, Job: job,
				Photo: str(obj(n, "image"), "large"),
			})
		}
	}
	return m
}

// crewJob maps an AniList staff role onto the host's small job vocabulary.
// Anything not named here (localization, animation, sound, assistants) is
// dropped rather than guessed at.
func crewJob(role string) string {
	switch strings.TrimSpace(role) {
	case "Director", "Chief Director":
		return "Director"
	case "Original Creator", "Original Story", "Story", "Script", "Series Composition":
		return "Writer"
	case "Producer", "Chief Producer":
		return "Producer"
	case "Executive Producer":
		return "Executive Producer"
	}
	return ""
}

func itoa(n int) string { return strconv.Itoa(n) }

func pad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// firstMedia runs a search and returns the best node, or nil.
func firstMedia(search string, year int, kind string, cfg map[string]string) (map[string]any, error) {
	vars := map[string]any{"search": search, "type": kind}
	if year > 0 {
		vars["year"] = year
	}
	if cfg["adult"] != "on" {
		vars["adult"] = false
	}
	out, err := post(searchQuery(), vars)
	if err != nil {
		return nil, err
	}
	nodes := arr(obj(obj(out, "data"), "Page"), "media")
	if len(nodes) == 0 {
		return nil, nil
	}
	node, _ := nodes[0].(map[string]any)
	return node, nil
}

// ---- episodes ----

// episodeQuery pulls everything needed to describe a season in one
// request: the streaming titles (which carry the episode names) and the
// airing schedule (which carries dates for anything recent enough to have
// one).
const episodeQuery = `query ($id: Int) {
  Media(id: $id) {
    id
    episodes
    streamingEpisodes { title thumbnail }
    airingSchedule(perPage: 100) { nodes { episode airingAt } }
  }
}`

const sequelQuery = `query ($id: Int) {
  Media(id: $id) {
    relations { edges { relationType node { id type format } } }
  }
}`

type epInfo struct {
	Title string `json:"t,omitempty"`
	Still string `json:"b,omitempty"`
	Date  string `json:"d,omitempty"`
}

// parseEpisodeTitle splits AniList's "Episode 12 - The Real Folk Blues"
// into its number and name. Episode names frequently contain their own
// hyphens, so only the FIRST separator after the number counts.
func parseEpisodeTitle(s string) (int, string) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(s), "Episode ")
	if !ok {
		return 0, ""
	}
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, ""
	}
	n, err := strconv.Atoi(rest[:i])
	if err != nil {
		return 0, ""
	}
	name := strings.TrimSpace(rest[i:])
	return n, strings.TrimSpace(strings.TrimPrefix(name, "-"))
}

// fetchEpisodes reads one media's episode list, preferring the cache. A
// miss re-fetches, which is also how a still-airing season picks up the
// episodes that did not exist last time (no clock needed).
func fetchEpisodes(mediaID int) (map[int]epInfo, error) {
	key := "eps:" + strconv.Itoa(mediaID)
	out := map[int]epInfo{}
	if raw := cacheGet(key); raw != "" {
		if err := json.Unmarshal([]byte(raw), &out); err == nil && len(out) > 0 {
			return out, nil
		}
	}
	res, err := post(episodeQuery, map[string]any{"id": mediaID})
	if err != nil {
		return nil, err
	}
	media := obj(obj(res, "data"), "Media")
	if media == nil {
		return nil, nil
	}
	// Sanity-check the streaming list against the entry's own episode
	// count before believing a word of it.
	//
	// AniList's streamingEpisodes come from a licensor feed, and for a
	// SEQUEL that feed is routinely the whole franchise. "Shingeki no
	// Kyojin Season 2" declares 12 episodes and carries 25 streaming
	// titles, all of them season ONE's, so trusting it would name every
	// season 2 episode after a season 1 episode. More titles than the
	// entry claims to have means the list is not this entry's, and no
	// titles at all is better than confidently wrong ones.
	declared := int(num(media, "episodes"))
	streaming := arr(media, "streamingEpisodes")
	if declared > 0 && len(streaming) > declared {
		logf("ignoring " + strconv.Itoa(len(streaming)) + " streaming titles for media " +
			strconv.Itoa(mediaID) + ", which declares only " + strconv.Itoa(declared) + " episodes")
		streaming = nil
	}
	for _, raw := range streaming {
		se, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		n, name := parseEpisodeTitle(str(se, "title"))
		if n == 0 || name == "" {
			continue
		}
		out[n] = epInfo{Title: name, Still: str(se, "thumbnail")}
	}
	if sched := obj(media, "airingSchedule"); sched != nil {
		for _, raw := range arr(sched, "nodes") {
			node, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			n := int(num(node, "episode"))
			at := int64(num(node, "airingAt"))
			if n == 0 || at == 0 {
				continue
			}
			info := out[n]
			info.Date = unixDate(at)
			out[n] = info
		}
	}
	// Drop anything that ended up with a date but no name: the host uses
	// the title as the episode's name, and a nameless result is a no-match.
	for n, info := range out {
		if info.Title == "" {
			delete(out, n)
		}
	}
	if len(out) > 0 {
		if b, err := json.Marshal(out); err == nil {
			cacheSet(key, string(b))
		}
	}
	return out, nil
}

// seasonMedia resolves which AniList entry a host season number means.
//
// AniList models each season as its own entry, so season 2 is a SEQUEL of
// season 1 rather than part of it. Walking the sequel chain is the honest
// mapping. When the walk runs out this returns 0 and the caller answers
// "no match", rather than labelling season 3 with season 1 episode titles.
func seasonMedia(seriesID string, season int) (int, error) {
	base, err := strconv.Atoi(seriesID)
	if err != nil {
		return 0, nil
	}
	if season <= 1 {
		return base, nil
	}
	key := "seq:" + seriesID + ":" + strconv.Itoa(season)
	if raw := cacheGet(key); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			return n, nil
		}
	}
	id := base
	for step := 1; step < season; step++ {
		res, err := post(sequelQuery, map[string]any{"id": id})
		if err != nil {
			return 0, err
		}
		next := 0
		for _, raw := range arr(obj(obj(obj(res, "data"), "Media"), "relations"), "edges") {
			edge, ok := raw.(map[string]any)
			if !ok || str(edge, "relationType") != "SEQUEL" {
				continue
			}
			node := obj(edge, "node")
			// Only a TV sequel continues a season count: movies, OVAs and
			// specials are side stories, not the next season.
			if str(node, "type") != "ANIME" || str(node, "format") != "TV" {
				continue
			}
			next = int(num(node, "id"))
			break
		}
		if next == 0 {
			return 0, nil
		}
		id = next
	}
	cacheSet(key, strconv.Itoa(id))
	return id, nil
}

// unixDate renders an air time as YYYY-MM-DD without importing time, which
// would drag timezone data into the module for no benefit.
func unixDate(at int64) string {
	y, m, d := civilFromDays(at / 86400)
	return pad(y, 4) + "-" + pad(m, 2) + "-" + pad(d, 2)
}

// civilFromDays is Howard Hinnant's days-to-civil algorithm.
func civilFromDays(z int64) (int, int, int) {
	z += 719468
	era := z / 146097
	if z < 0 {
		era = (z - 146096) / 146097
	}
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if mp >= 10 {
		m = mp - 9
	}
	if m <= 2 {
		y++
	}
	return int(y), int(m), int(d)
}

// ---- people ----

const staffQuery = `query ($id: Int) {
  Staff(id: $id) {
    id
    name { full native }
    description(asHtml: false)
    image { large }
    dateOfBirth { year month day }
    homeTown
    primaryOccupations
    characterMedia(perPage: 24, sort: [POPULARITY_DESC]) {
      edges {
        node { id type title { romaji english } seasonYear coverImage { large } }
        characters { name { full } }
      }
    }
  }
}`

// staffPerson answers the host's people page for a voice actor or a crew
// member, using the same AniList staff ids the cast credits carry.
func staffPerson(id int) (map[string]any, error) {
	res, err := post(staffQuery, map[string]any{"id": id})
	if err != nil {
		return nil, err
	}
	st := obj(obj(res, "data"), "Staff")
	if st == nil {
		return nil, nil
	}
	name := ""
	if n := obj(st, "name"); n != nil {
		if name = str(n, "full"); name == "" {
			name = str(n, "native")
		}
	}
	if name == "" {
		return nil, nil
	}
	out := map[string]any{
		"id":        int64(num(st, "id")),
		"name":      name,
		"biography": stripHTML(str(st, "description")),
		"photo":     str(obj(st, "image"), "large"),
	}
	if d := obj(st, "dateOfBirth"); d != nil && num(d, "year") > 0 {
		out["birthday"] = pad(int(num(d, "year")), 4) + "-" + pad(int(num(d, "month")), 2) + "-" + pad(int(num(d, "day")), 2)
	}
	if h := str(st, "homeTown"); h != "" {
		out["placeOfBirth"] = h
	}
	for _, o := range arr(st, "primaryOccupations") {
		if s, ok := o.(string); ok && s != "" {
			out["knownForDepartment"] = s
			break
		}
	}
	var credits []map[string]any
	for _, raw := range arr(obj(st, "characterMedia"), "edges") {
		edge, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		node := obj(edge, "node")
		if str(node, "type") == "MANGA" {
			continue // the host's people pages are about screen credits
		}
		title := ""
		if t := obj(node, "title"); t != nil {
			if title = str(t, "romaji"); title == "" {
				title = str(t, "english")
			}
		}
		if title == "" {
			continue
		}
		character := ""
		for _, c := range arr(edge, "characters") {
			if cm, ok := c.(map[string]any); ok {
				if cn := obj(cm, "name"); cn != nil {
					character = str(cn, "full")
				}
			}
			break
		}
		credits = append(credits, map[string]any{
			"id": int64(num(node, "id")), "mediaType": "tv", "title": title,
			"year": int(num(node, "seasonYear")), "character": character,
			"poster": str(obj(node, "coverImage"), "large"),
		})
	}
	if len(credits) > 0 {
		out["credits"] = credits
	}
	return out, nil
}

// ---- dispatch ----

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
}

func reply(v any) int64 {
	out, err := json.Marshal(map[string]any{"data": v})
	if err != nil {
		return fail("could not encode the reply")
	}
	return packed(out)
}

func fail(msg string) int64 {
	out, _ := json.Marshal(map[string]any{"error": msg})
	return packed(out)
}

//go:wasmexport mp_handle
func mpHandle(ptr, n int32) int64 {
	var req struct {
		Op   string          `json:"op"`
		Data json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal(read(ptr, n), &req); err != nil {
		return fail("malformed request")
	}
	var a args
	json.Unmarshal(req.Data, &a)
	cfg := settings()

	switch req.Op {
	case "match.movie", "match.series":
		term, kind, isSeries := a.Title, "ANIME", false
		if req.Op == "match.series" {
			term, isSeries = a.Name, true
		}
		if term == "" {
			return reply(nil)
		}
		node, err := firstMedia(term, a.Year, kind, cfg)
		if err != nil {
			logf("search failed: " + err.Error())
			return fail(err.Error())
		}
		return reply(toMeta(node, cfg, isSeries))

	case "match.movieById", "match.seriesById":
		id, err := strconv.Atoi(a.ID)
		if err != nil {
			return reply(nil)
		}
		out, err := post(byIDQuery(), map[string]any{"id": id})
		if err != nil {
			return fail(err.Error())
		}
		return reply(toMeta(obj(obj(out, "data"), "Media"), cfg, req.Op == "match.seriesById"))

	case "match.episode":
		if a.Season == 0 {
			return reply(nil) // specials are not numbered within a season
		}
		mediaID, err := seasonMedia(a.SeriesID, a.Season)
		if err != nil {
			return fail(err.Error())
		}
		if mediaID == 0 {
			return reply(nil) // no TV sequel for that season: better than wrong titles
		}
		eps, err := fetchEpisodes(mediaID)
		if err != nil {
			return fail(err.Error())
		}
		info, ok := eps[a.Episode]
		if !ok || info.Title == "" {
			return reply(nil)
		}
		out := map[string]any{
			"id":       strconv.Itoa(mediaID) + "-" + itoa(a.Episode),
			"title":    info.Title,
			"backdrop": info.Still,
		}
		if len(info.Date) >= 4 {
			out["releaseDate"] = info.Date
			if y, err := strconv.Atoi(info.Date[:4]); err == nil {
				out["year"] = y
			}
		}
		return reply(out)

	case "match.search":
		if a.Query == "" {
			return reply([]any{})
		}
		vars := map[string]any{"search": a.Query, "type": "ANIME"}
		if a.Year > 0 {
			vars["year"] = a.Year
		}
		if cfg["adult"] != "on" {
			vars["adult"] = false
		}
		out, err := post(searchQuery(), vars)
		if err != nil {
			return fail(err.Error())
		}
		var results []map[string]any
		for _, raw := range arr(obj(obj(out, "data"), "Page"), "media") {
			node, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			m := toMeta(node, cfg, a.Kind == "tv")
			if m == nil {
				continue
			}
			results = append(results, map[string]any{
				"id": m.ID, "title": m.Title, "year": m.Year,
				"overview": m.Overview, "poster": m.Poster,
			})
		}
		return reply(results)

	case "match.images":
		id, err := strconv.Atoi(a.ID)
		if err != nil {
			return reply(nil)
		}
		out, err := post(byIDQuery(), map[string]any{"id": id})
		if err != nil {
			return fail(err.Error())
		}
		node := obj(obj(out, "data"), "Media")
		if node == nil {
			return reply(nil)
		}
		set := map[string][]string{}
		if c := obj(node, "coverImage"); c != nil {
			for _, k := range []string{"extraLarge", "large"} {
				if v := str(c, k); v != "" {
					set["posters"] = append(set["posters"], v)
				}
			}
		}
		if v := str(node, "bannerImage"); v != "" {
			set["backdrops"] = append(set["backdrops"], v)
		}
		return reply(set)

	case "match.person":
		id, err := strconv.Atoi(a.ID)
		if err != nil || id == 0 {
			return reply(nil)
		}
		person, err := staffPerson(id)
		if err != nil {
			return fail(err.Error())
		}
		return reply(person)
	}
	return fail("this plugin does not handle " + req.Op)
}

func main() {}
