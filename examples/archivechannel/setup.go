//go:build wasip1

// The moment after install, when eight libraries are about to be created
// and pointed at a public service nobody here controls (D179).
//
// The manifest declares `"setup": true`, which is what asks the host to
// call this. What it does NOT do is create anything: the eight libraries
// come from the channel block in the manifest, and the host makes them
// with no plugin code running at all. That is deliberate and it is why
// this file can be small. Setup is for the plugin's OWN work.
//
// So what is honest work here? The host is about to start eight first
// syncs against archive.org, and every one of them will take minutes. If
// the service is down, or a collection has been renamed out from under
// this plugin, all eight fail one after another and the owner reads eight
// identical errors. A preflight is two round trips and turns that into one
// sentence beside the plugin saying what is actually wrong.
//
// It checks the two endpoints the whole plugin stands on, which are also
// the two an owner cannot check for themselves:
//
//	advancedsearch.php   the index every catalog pass enumerates
//	metadata/<id>        the file list every Play resolves through
//
// Both under the plugin's own 30 second budget, with room to spare: two
// requests, about a second in total. Setup is not a place to sync a
// catalog, and this does not try to be one.
//
// A failure here leaves the plugin installed and visibly not set up, and
// the owner can press Retry once archive.org is answering again. Nothing
// is rolled back, because a transient blip is not a reason to throw away a
// working install.
package main

import (
	"encoding/json"
	"strconv"
)

// setupRequest is what the host tells setup: why it is running, where an
// upgrade is coming from, and the household's language.
type setupRequest struct {
	Reason          string `json:"reason"`
	PreviousVersion string `json:"previousVersion,omitempty"`
	Locale          string `json:"locale,omitempty"`
}

// setupResult is the answer. `ok:false` is a failed setup exactly as an
// error is, and `message` is the sentence shown beside the plugin, so it
// is written for a person and not for a log.
type setupResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// maxSetupMessage is where the host clips the message. Knowing the number
// is the difference between a sentence and a sentence with its end cut
// off, so the assembly below drops a clause rather than overrunning it.
const maxSetupMessage = 200

// probeLibrary is the library the index is checked through: the biggest of
// the eight, so its count is the most useful number to report, and the one
// the hero draws from, so a failure here is a failure somebody sees first.
const probeLibrary = "films"

// pluginSetup answers plugin.setup.
func pluginSetup(raw json.RawMessage) response {
	var req setupRequest
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			return response{Error: "unreadable setup request"}
		}
	}
	// Whatever happens below, the row goes away when this returns. A
	// progress row left behind by work that has stopped is a line on
	// somebody's activity list that means nothing.
	defer reportDone()

	l, ok := libByKey(probeLibrary)
	if !ok {
		return response{Error: "this plugin has no library called " + probeLibrary}
	}

	report(progressReport{Label: "Checking archive.org", Detail: "the search index", Pct: 0.15})
	var res searchResult
	// rows=1 because the count is the answer and a document is only proof
	// the query really ran. Asking for a page of results here would be
	// asking the service to build something nothing reads.
	if err := get(searchURLRows(l, nil, 1, 1), &res); err != nil {
		return failed("archive.org did not answer its search index, so the libraries cannot fill yet. " + err.Error())
	}
	if res.Response.NumFound <= 0 {
		return failed("archive.org answered, but the " + l.name +
			" collection came back empty, which means it has been renamed or emptied.")
	}

	p, ok := probeTitle()
	if !ok {
		return response{Error: "this plugin vouches for no titles, so there is nothing to check against"}
	}
	report(progressReport{Label: "Checking archive.org", Detail: "a title's file list", Pct: 0.6})
	meta, err := itemMeta(p.ia)
	if err != nil {
		return failed("archive.org answered its index but not its file lists, so nothing here would play yet. " + err.Error())
	}

	return response{Data: setupResult{OK: true,
		Message: setupMessage(req, l.name, res.Response.NumFound, p.title, len(meta.playableFiles()))}}
}

// failed is a setup that did not work. It is `ok:false` rather than an
// envelope error because the message is the useful part: the host records
// it, the hub shows it beside the plugin, and Retry is one press.
func failed(why string) response {
	return response{Data: setupResult{OK: false, Message: bound(why, maxSetupMessage)}}
}

// probeTitle is the title the file-list endpoint is checked through: the
// first of the hero picks, because that is the one an owner will see on
// the front of the channel and therefore the one worth knowing about.
func probeTitle() (pick, bool) {
	if hs := heroPicks(); len(hs) > 0 {
		return hs[0], true
	}
	if ps := picksFor(probeLibrary); len(ps) > 0 {
		return ps[0], true
	}
	return pick{}, false
}

// setupMessage writes the one sentence the owner reads. It carries the two
// numbers the checks actually produced rather than saying "OK": a count
// somebody can compare next month is worth more than a tick.
func setupMessage(req setupRequest, library string, found int, title string, copies int) string {
	head := ""
	if req.Reason == "upgrade" && req.PreviousVersion != "" {
		// The one thing an upgrade knows that an install does not, said
		// out loud so it is obvious the version arrived.
		head = "Upgraded from " + req.PreviousVersion + ". "
	}
	head += "archive.org is answering: " + strconv.Itoa(found) + " reviewed titles behind " + library
	title = bound(title, 40)
	if copies == 0 {
		// Not a failed setup: the service answered, which is what was
		// being checked. It is still worth saying, because a curated title
		// that lost its copies is a hole in the shelf somebody will notice.
		return bound(head+", but "+title+" has no playable copy today.", maxSetupMessage)
	}
	noun := " playable copies"
	if copies == 1 {
		noun = " playable copy"
	}
	head += ", and " + title + " still lists " + strconv.Itoa(copies) + noun + "."
	// The reassurance goes on only if it fits whole. A clipped sentence
	// reads as a truncation bug, and this one is the least important part.
	if tail := " The eight libraries can fill."; len([]rune(head+tail)) <= maxSetupMessage {
		head += tail
	}
	return bound(head, maxSetupMessage)
}
