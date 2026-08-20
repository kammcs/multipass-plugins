# Multipass plugins

Worked examples and the published contract for writing a
[Multipass](https://multipass.theater) plugin.

A plugin adds something to somebody's media server: a metadata source for a
collection nothing else identifies, extra detail on a film's page, a
scrobbler that keeps a watch history in step. It runs sandboxed inside the
server, ships as one file, and works on every screen without you writing
anything for any of them.

- **Guide**: <https://multipass.theater/plugins>
- **API reference**: <https://multipass.theater/plugins/reference>

## What is in here

| | |
|---|---|
| `examples/` | Five plugins, from the smallest possible one to two real services |
| `plugin-api.json` | The published contract, machine-readable. What the reference page is rendered from |
| `manifest.schema.json` | JSON Schema for `manifest.json`, so your editor validates as you type |

## The examples

Start by copying whichever is closest to what you want.

| Example | What it shows |
|---|---|
| `hello` | The ABI and nothing else. No permissions at all. Read this one first |
| `matchdemo` | A matching service with no network, so you can see the family's shape without an API in the way |
| `anilist` | A real matching service against a public API: the `http` grant, caching through `storage`, and settings that change behaviour |
| `eventdemo` | An event consumer with no network: linking, and what a delivery looks like |
| `trakt` | A real scrobbler: device-code sign-in per person, token refresh, and what to do when the far end revokes an account |

Every one of these is built and run by the server's own test suite on
every change, so none of them can quietly rot. What is published here is
what passed.

## Building one

Any language that compiles to WebAssembly works. The contract is two
exports and a linear memory, with nothing language-specific in it: the
server's own tests run hand-assembled modules that contain no runtime at
all.

The examples are Go, because Go needs no toolchain beyond Go itself:

```
GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o plugin.wasm .
```

Other toolchains that emit WASI reactor modules (TinyGo, Rust, C, Zig)
should work the same way. We have not built an example in one yet, so we
are not going to claim they do. If you get one working, we would like to
see it.

JavaScript and Python are the exception and cannot be used today. Neither
compiles to WebAssembly; both need an interpreter inside the module, and
the obvious tool for JS ([Javy](https://github.com/bytecodealliance/javy))
cannot export functions that take parameters or return values, which the
contract needs. Making JS work means shipping a QuickJS shell, and that is
not built yet.

## Packaging

```
multipass plugins pack ./myplugin -out myplugin.mpp
```

`manifest.json` is the document that gets signed, and it names every other
file with its hash. You do not write the `files` table; `pack` does.

To try it on a real server, turn on developer mode
(`MULTIPASS_PLUGIN_DEV=1`, set on the server itself), which adds an
**Install from file** control to Settings, Plugins. The server announces
developer mode at every boot, because a server that will run unsigned code
should never do so quietly.

## Submitting

Listing is free and hosting is free. Send the built `.mpp`, the source it
came from and how to build it, and one line on why it needs each
permission it asks for, to **support@kammcs.com** with *plugin submission*
in the subject. A person reads it, so expect a conversation rather than a
verdict. The rules are on the
[guide](https://multipass.theater/plugins#submit).

## About this repository

It is a mirror. These examples live inside the Multipass server tree,
where the test suite builds and runs them, and they are published here on
each release. That is deliberate: examples that are continuously proven
are worth more than examples anyone can edit, and stale examples are worse
than none.

So pull requests here cannot be merged directly: a commit would be
overwritten by the next publish, which would quietly throw your work away
a week later. Ones opened anyway are closed automatically with a pointer
back, which is not a brush-off, just the only honest answer to a branch
that cannot survive.

Issues are very welcome, and a patch in an issue gets applied upstream
with credit. See [CONTRIBUTING.md](CONTRIBUTING.md). An example in another
language is the most useful thing anyone could send.

## Licence

The examples are MIT (see `LICENSE`), so you can take any of them as a
starting point without owing anyone anything. Multipass itself is
proprietary; this licence covers the contents of this repository only.
