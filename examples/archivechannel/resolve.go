//go:build wasip1

// Play: turning one row in a library back into an address.
//
// Two shapes of id arrive here and `splitEpisodeID` tells them apart,
// because this plugin owns two shapes of library (D178 section 7):
//
//   - a MOVIE is a bare archive identifier, and the file has not been
//     chosen yet. The seven movie libraries sync from the search index
//     alone, so this is where the one metadata call per title is spent:
//     at Play, for the one title somebody actually pressed, rather than
//     two thousand times a sync for a catalog nobody is watching.
//   - an EPISODE is `<identifier>#<file name>`, and the file was chosen at
//     sync by shows.go. Nothing is ranked here; the name is the answer.
//
// The address is `archive.org/download/<id>/<file>`, which is what the
// archive publishes and which redirects to whichever machine holds the
// bytes today. The server settles that redirect itself, so a phone or a
// TV never talks to archive.org.
package main

import (
	"encoding/json"
	"strings"
	"time"
)

// streamTTL is how long the host may reuse an address before asking
// again. The archive's download addresses are not signed and do not
// expire, so this is not a deadline anybody set: a day is simply the
// point at which asking again is cheaper than being wrong about a file
// list that somebody re-uploaded in the meantime.
const streamTTL = 24 * 60 * 60

// playable ranks the files of one item, best first.
//
// Lifted from examples/plugins/archive 1.0.0 unchanged, because it is
// proven and because the reason still holds: the archive derives an MP4
// for nearly everything it keeps, and that derivative is the one to take.
// The original is as often a 380 MB MPEG-2 or a Cinepak AVI, and while
// the server can remux or transcode either, it should not have to for a
// title that already has a copy ready to stream.
//
// An empty `format` means the extension alone decides, which is how the
// tail of this table catches an item that has no h.264 derivative yet.
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
	if ia, file := splitEpisodeID(id); file != "" {
		return episodeStream(ia, file)
	}
	return movieStream(id)
}

// movieStream reads the item's file list and takes the best copy.
func movieStream(ia string) response {
	meta, err := itemMeta(ia)
	if err != nil {
		// An honest failure, in words. The host shows this to whoever
		// pressed Play, so it has to be a sentence they can act on rather
		// than a status code they have to look up.
		return response{Error: "could not read that title from the archive: " + err.Error()}
	}
	file := bestFile(meta)
	if file == "" {
		return response{Error: "that title has no copy this server can play"}
	}
	return streamAt(ia, file)
}

// episodeStream builds the address for a file that was already chosen,
// after checking it is still there.
//
// The check costs one metadata call, which is exactly what a movie costs,
// and it buys the difference between two failures. Uploaders DO rename
// files: a season gets re-derived, somebody fixes the spelling of a
// title, and every `#<file name>` id minted from the old list stops
// pointing at anything. Without the check that lands on a viewer as a
// player that spins and dies on an archive 404, with nothing anywhere
// saying why. With it, the person is told what happened and what fixes
// it, and the next complete sync really does fix it.
//
// What this deliberately does NOT do is fall back to the item's best
// playable file. On a series item that is episode one of a hundred and
// sixty-nine, so a fallback would quietly play the wrong episode, which
// is worse than not playing at all: a failure a person can see is
// recoverable and a silent substitution is not.
func episodeStream(ia, file string) response {
	meta, err := itemMeta(ia)
	if err != nil {
		return response{Error: "could not read that episode from the archive: " + err.Error()}
	}
	for _, f := range meta.Files {
		if f.Name == file {
			return streamAt(ia, file)
		}
	}
	return response{Error: "the archive no longer has a file by that name in this series, " +
		"which usually means the uploader renamed it. It should come back under its new name " +
		"after the next sync."}
}

func streamAt(ia, file string) response {
	return response{Data: resolved{
		URL:       downloadURL(ia, file),
		ExpiresAt: time.Now().Unix() + streamTTL,
		Hint:      strings.TrimPrefix(ext(file), "."),
	}}
}

// bestFile walks the ranking table in order, so the archive's own h.264
// derivative wins over an MP4 that merely ends in .mp4, and both win over
// whatever the uploader posted in 1999.
//
// The format test is a PREFIX, for the reason main.go's playableFiles
// gives: the archive files its derivatives as `h.264` on one item and
// `h.264 IA` on the next, and an exact compare quietly demotes the second
// kind to the plain-extension rung. It still played, because a `.ia.mp4`
// ends in `.mp4`, which is exactly what makes this class of bug survive a
// test that only asks whether something came back.
func bestFile(m iaMeta) string {
	for _, want := range playable {
		for _, f := range m.Files {
			if f.Name == "" || ext(f.Name) != want.ext {
				continue
			}
			if want.format != "" && !strings.HasPrefix(strings.ToLower(f.Format), want.format) {
				continue
			}
			return f.Name
		}
	}
	return ""
}
