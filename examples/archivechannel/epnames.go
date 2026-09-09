//go:build wasip1

// Reading a season, an episode and a title out of a file name.
//
// This is the half of the show library that has no contract in it, only
// judgement. An archive item's files were named by whoever uploaded them,
// so the input is `Green Acres Season 1/Green Acres - 001 - Oliver Buys A
// Farm.mp4` on one item, `Shogun 1.mp4` on the next, `s01e03` on a third
// and `KOLCHAK_DISC4.Title7.mp4` on a fourth.
//
// Two rules hold the whole file together:
//
//   - a file is NEVER dropped for being unreadable. It takes its position
//     in the list instead, stepping over any slot a parsed file already
//     claimed. An episode in the wrong slot is something a person can fix
//     or ignore; an episode that does not exist is not.
//   - the numbering has to be stable for a given file list, because season
//     plus episode plus series is how somebody finds a show again after a
//     re-sync.
//
// It lives apart from shows.go because that file is the contract (a page,
// a series, its episodes) and this one is a pile of string handling that
// grew every time it met a real item. Splitting at the seam keeps both
// under the house 600-line cap.
package main

import (
	"strconv"
	"strings"
)

// ---- season and episode ----

// pathNumbers is what one file's path says about where its episode sits.
// ordinal is kept apart from episode because a file numbered inside a season
// folder may be numbered across the whole run instead, and that is only
// visible once every file in the item has been read.
type pathNumbers struct {
	season  int
	episode int
	ordinal int
	tagged  bool
}

// numberEpisodes gives every playable file a season and an episode.
//
// The last rule is the important one: a file nobody can parse still gets a
// number, because an episode in the wrong slot is something a person can fix
// and an episode that does not exist is not. It takes the file's position in
// the list, stepping over a slot a parsed file already claimed, so the one
// unreadable extra in an otherwise clean run lands after the season rather
// than on top of its first episode. Every rule here reads only the file list,
// so the same list always numbers the same way, which is what lets somebody
// find an episode again after a re-sync.
func numberEpisodes(files []iaFile) []pathNumbers {
	out := make([]pathNumbers, len(files))
	for i, f := range files {
		out[i] = readPath(f.Name)
	}
	fixAbsoluteOrdinals(out)
	taken := map[[2]int]bool{}
	for _, p := range out {
		if p.season > 0 && p.episode > 0 {
			taken[[2]int{p.season, p.episode}] = true
		}
	}
	for i := range out {
		if out[i].season <= 0 {
			out[i].season = 1
		}
		if out[i].episode > 0 {
			continue
		}
		e := i + 1
		for taken[[2]int{out[i].season, e}] {
			e++
		}
		out[i].episode, taken[[2]int{out[i].season, e}] = e, true
	}
	return out
}

func readPath(name string) pathNumbers {
	dirs, base := dirBase(name)
	base = dropExt(base)
	// An outright tag is the uploader saying it in so many words, so it beats
	// anything a folder or a loose number implies.
	if s, e, ok := episodeTag(base); ok {
		return pathNumbers{season: s, episode: e, tagged: true}
	}
	// "Forensic Files - Season 2, Episode 11 - Postal Mortem" is the same
	// statement written out, and reading the first loose number there files
	// it as episode 2.
	if e := wordNumber(base, "episode"); e > 0 {
		s := wordNumber(base, "season")
		if s == 0 {
			s = folderSeason(dirs)
		}
		return pathNumbers{season: s, episode: e, tagged: true}
	}
	n := pathNumbers{season: folderSeason(dirs), ordinal: firstOrdinal(base)}
	n.episode = n.ordinal
	return n
}

// fixAbsoluteOrdinals handles the numbering that looks per-season and is not.
// GreenAcresCompleteSeries files 169 episodes under six season folders and
// numbers them 001 to 170 STRAIGHT THROUGH: season 6 holds 145 to 170. At
// face value that files episode 168 of season 6, which no metadata service
// will ever match, in the one library whose point is that a curated series
// enriches per episode.
//
// The tell is that a per-season numbering restarts, so when every season
// above the lowest starts above 1 the ordinal is a series-wide count and the
// episode is its offset inside its own season. Subtracting the season's own
// minimum rather than counting positions keeps the gaps, so a run missing an
// episode still lines up with the numbers it was broadcast under.
func fixAbsoluteOrdinals(n []pathNumbers) {
	base := map[int]int{}
	for _, p := range n {
		if p.tagged || p.season <= 0 || p.ordinal <= 0 {
			continue
		}
		if cur, ok := base[p.season]; !ok || p.ordinal < cur {
			base[p.season] = p.ordinal
		}
	}
	if len(base) < 2 {
		return
	}
	first := 0
	for s := range base {
		if first == 0 || s < first {
			first = s
		}
	}
	for s, lowest := range base {
		if s != first && lowest <= 1 {
			return
		}
	}
	for i, p := range n {
		if !p.tagged && p.season > 0 && p.ordinal > 0 {
			n[i].episode = p.ordinal - base[p.season] + 1
		}
	}
}

// folderSeason reads the season off the directories, last one wins, since an
// uploader who nests "Green Acres Season 3/Green Acres Season 3/" is saying
// the same thing twice.
func folderSeason(dirs []string) int {
	season := 0
	for _, d := range dirs {
		if n := seasonInName(d); n > 0 {
			season = n
		}
	}
	return season
}

// seasonInName matches "Season 1", "Green Acres Season 1", "Series 1" and a
// bare "S3", and deliberately matches nothing else. The folders here are also
// episode titles ("Bad Medicine/"), disc labels ("KOLCHAK_DISC2/") and
// container names ("avi/"), and reading a season out of any of those puts a
// whole series in the wrong place.
func seasonInName(d string) int {
	for _, word := range []string{"season", "series"} {
		if n := wordNumber(d, word); n > 0 {
			return n
		}
	}
	t := strings.TrimSpace(d)
	if len(t) > 1 && (t[0] == 's' || t[0] == 'S') {
		if v, k, next := epNumber(t, 1); k > 0 && k <= 2 && v > 0 && next == len(t) {
			return v
		}
	}
	return 0
}

// wordNumber reads the number that follows a word: the 2 in "Season 2", the
// 11 in "Episode 11". Separators then digits are required, so "(13th Season)"
// and "Complete Series" report nothing.
func wordNumber(s, word string) int {
	low := strings.ToLower(s)
	for from := 0; from+len(word) <= len(low); {
		i := strings.Index(low[from:], word)
		if i < 0 {
			return 0
		}
		i += from
		j := i + len(word)
		for j < len(low) && epSep(low[j]) {
			j++
		}
		if v, k, _ := epNumber(low, j); k > 0 && k <= 3 && v > 0 {
			return v
		}
		from = i + len(word)
	}
	return 0
}

// episodeTag reads an outright "s01e02" or "2x05" out of a file name.
//
// The season half may start mid-word because Bonanza_pd names half its files
// "Bonanza s01e19.mp4" and the other half "BonanzaS02e17.mp4", and the second
// half is not worth losing. A digit in front is still refused, so a
// resolution or a year cannot open a tag.
func episodeTag(s string) (int, int, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != 's' && s[i] != 'S' || i > 0 && s[i-1] >= '0' && s[i-1] <= '9' {
			continue
		}
		se, k, j := epNumber(s, i+1)
		if k == 0 || k > 2 || se == 0 {
			continue
		}
		for j < len(s) && epSep(s[j]) {
			j++
		}
		if j >= len(s) || s[j] != 'e' && s[j] != 'E' {
			continue
		}
		if ep, k2, _ := epNumber(s, j+1); k2 > 0 && k2 <= 3 && ep > 0 {
			return se, ep, true
		}
	}
	for i := 0; i < len(s); i++ {
		if i > 0 && !epSep(s[i-1]) {
			continue
		}
		se, k, j := epNumber(s, i)
		if k == 0 || k > 2 || se == 0 || j >= len(s) || s[j] != 'x' && s[j] != 'X' {
			continue
		}
		// The tag has to end here, or "x264" in a release name reads as an
		// episode number.
		if ep, k2, end := epNumber(s, j+1); k2 > 0 && k2 <= 3 && ep > 0 &&
			(end == len(s) || epSep(s[end])) {
			return se, ep, true
		}
	}
	return 0, 0, false
}

// firstOrdinal is the first standalone number in a file name: the 001 in
// "Green Acres - 001 - Oliver Buys A Farm", the 01 in "UFO.01.Identified",
// the 1 in "Shogun 1", the 1 in "Ken.Burns.The.Civil.War.1of9".
//
// Three digits is the ceiling on purpose. A four digit number in a file name
// is a year ("Real Sex 01 (1990)"), and a year read as an episode number
// files the episode nineteen hundred and something.
func firstOrdinal(s string) int {
	prev := ""
	for _, t := range epTokens(s) {
		if n, ok := partOf(t); ok {
			return n
		}
		// A number behind "Season" is naming the season. Without this,
		// "The Bob Newhart Show Season 4 Featurette.mp4" read 4 as an
		// ordinal and dropped one featurette on top of each season opener.
		label := strings.EqualFold(prev, "season") || strings.EqualFold(prev, "series")
		if v, k, next := epNumber(t, 0); k > 0 && k <= 3 && next == len(t) && v > 0 && !label {
			return v
		}
		prev = t
	}
	return 0
}

// partOf reads "1of9", which is how a documentary series names its parts.
func partOf(t string) (int, bool) {
	v, k, i := epNumber(t, 0)
	if k == 0 || k > 3 || v == 0 || i+2 > len(t) || !strings.EqualFold(t[i:i+2], "of") {
		return 0, false
	}
	if _, k2, end := epNumber(t, i+2); k2 > 0 && end == len(t) {
		return v, true
	}
	return 0, false
}

// ---- episode titles ----

// episodeTitles derives a readable title per file and then throws away the
// ones that turned out not to be titles. "01 - Benny Hill.mp4" through
// "78 - Benny Hill.mp4" all reduce to the show's own name, and seventy-eight
// rows called "Benny Hill" is worse than seventy-eight numbered ones, so a
// title that is not unique inside the item becomes its episode number.
func episodeTitles(files []iaFile, series string, nums []pathNumbers) []string {
	out := make([]string, len(files))
	seen := map[string]int{}
	for i, f := range files {
		_, base := dirBase(f.Name)
		if t := strings.TrimSpace(f.Title); t != "" {
			base = t
		}
		out[i] = cleanEpisodeTitle(base, series)
		if out[i] != "" {
			seen[strings.ToLower(out[i])]++
		}
	}
	for i := range out {
		// Never an empty title: the host drops a row that has none, and a
		// dropped row is an episode nobody can play.
		if out[i] == "" || seen[strings.ToLower(out[i])] > 1 {
			out[i] = "Episode " + strconv.Itoa(nums[i].episode)
		}
	}
	return out
}

// titleWords are the labels around a number, stripped off the front once the
// number itself has been read.
var titleWords = []string{"season", "series", "episode", "ep", "part", "disc"}

func cleanEpisodeTitle(name, series string) string {
	key := epNormalize(series)
	s := cutSeriesPrefix(trimSeps(epNormalize(dropExt(name))), key)
	// Whatever is left in front of the words is numbering: an ordinal, a
	// season tag, a part-of count, a release year, or the label in front of
	// one. Six passes covers "Season 1, Episode 1 - " with room over.
	for n := 0; n < 6; n++ {
		head, _, next := nextToken(s, 0)
		if head == "" || !junkToken(head) {
			break
		}
		s = trimSeps(s[next:])
	}
	s = trimSeps(sceneCut(s))
	// "Get Smart S01E01 (Mr. Big)" keeps its episode title in brackets, and
	// taking the tag off the front takes the opening bracket with it.
	if strings.Count(s, "(") != strings.Count(s, ")") {
		s = trimSeps(strings.Trim(s, "()"))
	}
	if s == "" || strings.EqualFold(s, key) {
		return ""
	}
	if _, k, end := epNumber(s, 0); k > 0 && end == len(s) {
		return "" // A bare number is a position, not a title.
	}
	return s
}

// cutSeriesPrefix takes the show's name off the front of a file name by whole
// words rather than by bytes: "Dragnet ( 1951)" as a title and "Dragnet
// (1951) - S03E24 - The Big Children" as a file name do not share a byte
// prefix, and they obviously share a name.
func cutSeriesPrefix(s, series string) string {
	want := epTokens(series)
	if len(want) == 0 {
		return s
	}
	end := 0
	for _, w := range want {
		t, _, next := nextToken(s, end)
		if t == "" || !strings.EqualFold(t, w) {
			return s
		}
		end = next
	}
	return trimSeps(s[end:])
}

func junkToken(t string) bool {
	for _, w := range titleWords {
		if strings.EqualFold(t, w) {
			return true
		}
	}
	if _, ok := partOf(t); ok {
		return true
	}
	if _, _, ok := episodeTag(t); ok {
		return true
	}
	if len(t) > 2 && strings.EqualFold(t[:2], "ep") {
		if _, k, end := epNumber(t, 2); k > 0 && end == len(t) {
			return true
		}
	}
	v, k, end := epNumber(t, 0)
	if k == 0 || end != len(t) {
		return false
	}
	return k <= 3 || k == 4 && sensibleYear(v) > 0
}

// sceneTags are what a release group appends. Cutting at the first one turns
// "Pride.and.Prejudice.1995.S01E01.720p.BluRay.x264-GalaxyTV" into something
// a person would read out loud.
var sceneTags = []string{
	"480p", "576p", "720p", "1080p", "2160p", "bluray", "brrip", "bdrip",
	"dvdrip", "webrip", "hdtv", "x264", "x265", "h264", "h265", "xvid",
	"divx", "aac", "ac3", "10bit",
}

func sceneCut(s string) string {
	for i := 0; i < len(s); {
		t, start, next := nextToken(s, i)
		if t == "" {
			break
		}
		for _, tag := range sceneTags {
			if strings.EqualFold(t, tag) {
				return s[:start]
			}
		}
		i = next
	}
	return s
}

// ---- small readers ----

// fileMinutes reads a file's declared duration. main.go's minutes() reads the
// clock form ("28:19") the SEARCH INDEX answers in; an item's FILE list
// answers in seconds instead, and all 876 playable files in the classic_tv
// sample on 2026-09-08 were seconds ("1520.14"). Both shapes have to be read
// or every episode in this library arrives with no runtime at all.
func fileMinutes(s string) int {
	if m := minutes(s); m > 0 {
		return m
	}
	t := strings.TrimSpace(s)
	if i := strings.IndexByte(t, '.'); i >= 0 {
		t = t[:i]
	}
	n, err := strconv.Atoi(t)
	if err != nil || n <= 0 {
		return 0
	}
	return n / 60
}

// epSep is what separates the words in a name somebody typed. Dots and
// underscores are separators here, not punctuation, and so are brackets,
// which keeps "(1990)" from reading as a bare number.
func epSep(c byte) bool {
	switch c {
	case ' ', '\t', '.', '_', '-', '(', ')', '[', ']', ',', ':', ';', '|':
		return true
	}
	return false
}

// epNumber reads the run of digits at i and reports how many there were, so a
// caller can tell a two digit episode from a four digit year.
func epNumber(s string, i int) (val, digits, next int) {
	n, k := 0, 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
		k++
	}
	return n, k, i
}

// nextToken reads the word at or after i, and reports where it began and
// where to carry on from.
func nextToken(s string, i int) (tok string, start, next int) {
	for i < len(s) && epSep(s[i]) {
		i++
	}
	j := i
	for j < len(s) && !epSep(s[j]) {
		j++
	}
	return s[i:j], i, j
}

func epTokens(s string) []string {
	out := []string{}
	for i := 0; i < len(s); {
		t, _, next := nextToken(s, i)
		if t != "" {
			out = append(out, t)
		}
		i = next
	}
	return out
}

// epNormalize turns a file name into the words it carries. Only ASCII bytes
// are touched, so a title in any other script comes through whole.
func epNormalize(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c == '_' || c == '.' {
			b.WriteByte(' ')
		} else {
			b.WriteByte(c)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// trimSeps clears the separators off both ends, and a stray closing bracket
// off the front: cutting "Dragnet (1951)" out of a file name leaves behind
// the bracket the show's own title had opened.
func trimSeps(s string) string {
	return strings.TrimRight(strings.TrimLeft(s, " \t-_.,:;|()[]"), " \t-_.,:;|")
}

func dirBase(name string) ([]string, string) {
	parts := strings.Split(name, "/")
	return parts[:len(parts)-1], parts[len(parts)-1]
}

// dropExt takes the file extension off, and only a plausible one: the second
// dot in "UFO.01.Identified.mp4" is a separator, not an extension. The `.ia`
// the archive's own derivatives carry ("Get Smart S01E01 (Mr. Big).ia.mp4")
// goes with it, since it names the deriver and not the episode.
func dropExt(s string) string {
	if e := ext(s); len(e) > 1 && len(e) <= 5 {
		s = s[:len(s)-len(e)]
	}
	if len(s) > 3 && strings.EqualFold(s[len(s)-3:], ".ia") {
		s = s[:len(s)-3]
	}
	return s
}
