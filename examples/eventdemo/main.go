//go:build wasip1

// The D165 events-family fixture: an event consumer with no network.
//
// It exists so the harness can prove the family's promises without a third
// party being up. It links (through a pretend device-code flow), records
// every delivery, and answers `demo.log` with what it has been told, which
// is how a test asserts who was and was not mentioned to it.
//
// The privacy property is the one worth watching in the harness: this
// plugin can only ever record profiles that connected themselves to it, so
// its own log is the proof that an unlinked profile was never named.
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

func unpack(v int64) []byte {
	if v == 0 {
		return nil
	}
	return read(int32(v>>32), int32(v))
}

//go:wasmimport mp config
func hostConfig() int64

//go:wasmimport mp storage
func hostStorage(ptr, n int32) int64

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

func store(op, key, value string) string {
	req, err := json.Marshal(map[string]any{"op": op, "key": key, "value": value})
	if err != nil {
		return ""
	}
	var out struct {
		Value string `json:"value"`
	}
	json.Unmarshal(unpack(hostStorage(int32(uintptr(unsafe.Pointer(&req[0]))), int32(len(req)))), &out)
	return out.Value
}

// ---- the plugin ----

const logKey = "deliveries"

// record appends one line to the plugin's own log. The storage grant is
// per-plugin, so this is the plugin's memory and nobody else's.
func record(line string) {
	prev := store("get", logKey, "")
	if prev != "" {
		line = prev + "\n" + line
	}
	store("set", logKey, line)
}

// linkBegin pretends to be a device-code flow. A real one asks a service;
// this returns a code from settings so a test can predict it.
func linkBegin() map[string]any {
	code := strings.TrimSpace(settings()["code"])
	if code == "" {
		code = "DEMO-1234"
	}
	state, _ := json.Marshal(map[string]any{"pendingCode": code})
	return map[string]any{
		"instructions": "Type this code where the service asks for it.",
		"url":          "https://example.com/activate",
		"code":         code,
		"expiresIn":    600,
		"interval":     3,
		"state":        json.RawMessage(state),
	}
}

// linkPoll answers pending once, then linked. Answering pending at least
// once matters: it is the state a real flow spends most of its time in,
// and a harness that never saw it would not be testing the wait.
func linkPoll(state map[string]any, profile map[string]any) map[string]any {
	code, _ := state["pendingCode"].(string)
	if code == "" {
		return map[string]any{"pending": true}
	}
	if _, polled := state["polled"]; !polled {
		state["polled"] = true
		next, _ := json.Marshal(state)
		return map[string]any{"pending": true, "state": json.RawMessage(next)}
	}
	name, _ := profile["name"].(string)
	next, _ := json.Marshal(map[string]any{"token": "demo-token-for-" + name})
	return map[string]any{
		"linked": true, "account": "demo:" + name, "state": json.RawMessage(next),
	}
}

// handleEvent records what it was told, and demonstrates the two things a
// consumer can say back: replacement state, and a note for the log.
func handleEvent(in map[string]any) map[string]any {
	event, _ := in["event"].(string)
	profile, _ := in["profile"].(map[string]any)
	item, _ := in["item"].(map[string]any)
	session, _ := in["session"].(map[string]any)

	name, _ := profile["name"].(string)
	title, _ := item["title"].(string)
	progress, _ := session["progress"].(float64)
	record(event + "|" + name + "|" + title + "|" + strconv.Itoa(int(progress+0.5)))

	out := map[string]any{"note": "recorded " + event + " for " + name}
	// A finish is where a real consumer would write a completed history
	// entry, so it is where this one proves state can be replaced.
	if event == "playback.finished" {
		next, _ := json.Marshal(map[string]any{"token": "demo-token-for-" + name, "lastFinished": title})
		out["state"] = json.RawMessage(next)
	}
	return out
}

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
	state := map[string]any{}
	if raw, present := in["state"]; present && raw != nil {
		b, _ := json.Marshal(raw)
		json.Unmarshal(b, &state)
	}
	profile, _ := in["profile"].(map[string]any)

	switch call.Op {
	case "link.begin":
		return ok(linkBegin())
	case "link.poll":
		return ok(linkPoll(state, profile))
	case "link.revoke":
		name, _ := profile["name"].(string)
		record("revoked|" + name + "||0")
		return ok(map[string]any{})
	case "event.deliver":
		return ok(handleEvent(in))
	case "demo.log":
		// Not part of the contract: a fixture affordance so the harness can
		// read back what this plugin was actually told.
		return ok(map[string]any{"log": store("get", logKey, "")})
	case "demo.clear":
		store("delete", logKey, "")
		return ok(map[string]any{})
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
