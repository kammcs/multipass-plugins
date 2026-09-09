//go:build wasip1

// Saying what is happening, while it is happening (D179 R5).
//
// This library's problem is that its slow work is INVISIBLE. A curated
// pass is a table and finishes in milliseconds, but Classic Television at
// `reviewed` depth is one metadata call per title, five hundred titles
// deep, and a first sync of it runs for the better part of an hour. Before
// `progress` the only thing an owner could see during that was a library
// whose count went up now and then.
//
// The rules are the host's and they are worth knowing rather than
// discovering:
//
//   - ONE ROW PER PLUGIN. A second call replaces the first, so this is a
//     current-state report and never a log. Reporting per title is fine;
//     reporting per title into a list would not be.
//   - `progress` is VOID. There is no answer to read and no failure to
//     handle: a host with nowhere to put the row drops it, which is what
//     `multipass plugins call` does.
//   - the row goes stale after ninety seconds without a refresh, so work
//     that stops reporting stops being on somebody's screen by itself.
//     Nothing here has to unwind a row after a crash.
//   - `done` clears it, and a pass that finishes says so rather than
//     waiting out the stale timer.
//
// It needs no capability, for the same reason `log` needs none: it is a
// report, not a power.
package main

import "encoding/json"

//go:wasmimport mp progress
func hostProgress(ptr, n int32)

// progressReport is the host's ProgressReport, field for field. `pct` is
// 0..1 and the host clamps it; `label` and `detail` are clipped to 80 and
// 120 runes there, which is why the ones built below are short by
// construction rather than by trusting the clip.
type progressReport struct {
	Label  string  `json:"label,omitempty"`
	Pct    float64 `json:"pct,omitempty"`
	Detail string  `json:"detail,omitempty"`
	Done   bool    `json:"done,omitempty"`
}

const (
	maxLabelRunes  = 80
	maxDetailRunes = 120
)

// report hands one row to the host. There is nothing to check afterwards:
// a report that could fail would be a report nobody dared make from inside
// a loop.
func report(p progressReport) {
	p.Label = bound(p.Label, maxLabelRunes)
	p.Detail = bound(p.Detail, maxDetailRunes)
	out, err := json.Marshal(p)
	if err != nil {
		return
	}
	ptr := mpAlloc(int32(len(out)))
	copy(live[ptr], out)
	hostProgress(ptr, int32(len(out)))
}

// reportDone clears this plugin's row. Every path out of a sync calls it,
// including the failing ones: a pass that died leaving "page 3 of 12" on
// an owner's activity list for ninety seconds is worse than no row at all.
func reportDone() { report(progressReport{Done: true}) }

// bound clips by runes, so a clip never cuts a character in half. It is
// deliberately not clip(): a progress row is not prose and an ellipsis in
// a one-line status reads as an error.
func bound(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// walk is where one page sits inside a whole enumeration, so a report can
// say how far along the PASS is rather than how far along one page is.
//
// The distinction is the whole value of the number. A show page is twenty
// titles and there are twenty-five of them, so a per-page percentage would
// run 0 to 100 twenty-five times and mean nothing. `total` of 0 means the
// size is not known, and then no percentage is sent at all: an honest
// indeterminate row beats a confident wrong one.
type walk struct {
	label  string
	offset int // titles finished before this page began
	total  int // titles in the whole enumeration, 0 when unknown
}

// at reports progress i titles into this page.
func (w walk) at(i int, detail string) {
	p := progressReport{Label: w.label, Detail: detail}
	if w.total > 0 {
		p.Pct = float64(w.offset+i) / float64(w.total)
	}
	report(p)
}
