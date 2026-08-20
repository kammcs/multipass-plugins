# Contributing

Short version: **open an issue, not a pull request.** A patch pasted into
an issue gets applied upstream with credit.

## Why pull requests cannot be merged here

This repository is a mirror. These examples live inside the Multipass
server tree, where the test suite builds and runs every one of them on
every change: four test harnesses use them as fixtures, so an example that
stopped working fails our CI rather than misleading somebody months later.

Publishing a copy here gets you a public repository to read, copy and file
against, without giving up the thing that makes the examples worth
reading. The cost is that a commit here would be overwritten by the next
mirror, so merging your pull request would quietly throw your work away a
week later. Better to say so than to let that happen.

Pull requests opened here are closed automatically with a pointer back to
this file. That is not a brush-off, and it is not personal: it is the only
honest response to a branch that cannot survive.

## What to open an issue about

- **A bug in an example.** Say which one and what it did.
- **A patch.** Paste the diff, or describe the change. It gets applied
  upstream, and the commit credits you by name or handle, whichever you
  prefer.
- **An example in another language.** Genuinely wanted. The contract is
  two exports and a linear memory with nothing language-specific in it, so
  TinyGo, Rust, C and Zig should all work; we have only built Go, which is
  why the README does not claim the others. A working example in one of
  them is the most useful thing anyone could send.
- **Something the contract cannot express.** If you are trying to build a
  plugin and the API is in the way, that is worth hearing about even
  without a fix attached.

## What to send somewhere else

- **Bugs in Multipass itself**, rather than in these examples:
  <support@kammcs.com>.
- **A plugin you want listed** in the registry: also
  <support@kammcs.com>, with *plugin submission* in the subject. Send the
  built `.mpp`, the source and how to build it, and one line on why it
  needs each permission it asks for. See the
  [guide](https://multipass.theater/plugins#submit).

## Licence

The examples are MIT, so anything you send is contributed under those
terms.
