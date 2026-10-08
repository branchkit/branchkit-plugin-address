# BranchKit Address

Say a file's name, and its address is typed where you are writing. "mention
mission" types `@docs/MISSION.md ` into Claude Code, which attaches the file
to your prompt. A plugin for [BranchKit](https://github.com/branchkit), an
accessibility plugin platform for the desktop. MIT licensed.

BranchKit is pre-launch: the app is not publicly released yet.

For someone who cannot type, prose is the solved part: dictation writes it.
Identifiers are what stay hard: `matching_service.rs`, `DEV_LOOP.md`,
`plugins/voice/src/main.go`. This plugin knows the names of the things you
work with and writes the address of the one you say. Files in Claude Code
come first; other things (branches, issues, notes) and other destinations
(a Markdown link, `[[Note]]`) are planned on the same core.

## What you can say

| Say | Does |
|---|---|
| `mention <file>` | Type the file's address, `@path` and a space |
| `mention` | List the files you changed or mentioned lately, each beside a word to say |
| the word beside a file (`aim`, `bus`, …) | Type that file, from the list or when a name could mean several files |
| `<file>`, while the list is open | Type that file: after "mention", file names and the list's words are all that is heard |
| `cancel` | Close the list without typing anything |

The list holds the files your working tree has changed, then the ones you
mentioned most recently in this project (since BranchKit started), at most 26,
so every word is one word. It is the way to a file whose name does not come
out right: say "mention", then its word.

## How files get their names

Every name is said in plain words, never punctuation or digits:

- A file is named by its own name, split where a word break is heard:
  `DEV_LOOP.md` is "dev loop", `matchingService.ts` "matching service".
- Words run together are also offered split: `README` is "readme" and
  "read me", `gitignore` "gitignore" and "git ignore".
- Capitalised abbreviations of up to four letters, and short ones without a
  vowel, are spelled: `MCP` is "m c p", `src` "s r c". A capitalised word
  stays a word: `MISSION` is "mission".
- Numbers are said naturally and digit by digit: `v2` is "v two" and
  "version two", `2026` "twenty twenty six" and "two zero two six".
- The extension is never needed. It only separates files that are otherwise
  equal: `names.rs` and `names.go` beside each other are "names rust" and
  "names go".
- Names that say what kind of file it is rather than which one (`mod`,
  `main`, `index`, `README` below the root, …) are always said with their
  folder: "sandbox mod".
- Files sharing a name are named by the shortest end of their path no other
  file shares, as VS Code labels editor tabs: "voice main" when only one
  `main` has a `voice` folder above it.
- One word of a file's name names it too, when no other file has that word:
  "matching" for `matching_service.rs`.
- Files changed in your working tree rank first, then files nearer the
  project root.

Whether a word is a word comes from an English word list derived from the CMU
Pronouncing Dictionary (`src/lexicon.txt.gz`, built by `src/genlexicon.go`),
with the entries it pronounces letter by letter (abbreviations like "api")
left out.

### When a name could mean several files

When every other file the name could mean is in a test, fixture or vendored
folder (`test`, `tests`, `__tests__`, `testdata`, `fixtures`,
`test-fixtures`, `mocks`, `vendor`, `third_party`, `node_modules`), the one
that is not is typed without asking: "mention mission" types
`docs/MISSION.md`, not a fixture called `mission.html`. The others keep their
longer names ("lifecycle mission"), so they can still be said.

Otherwise saying it shows the files, the most likely first, each badged with
a word from BranchKit's alphabet (the same words browser hints and every
other list use). Say the word (in the same hold) and that file's address is
typed. Without the alphabet (it is published by the voice plugin) the badges
are numbers. The list is a card at the top centre of the screen, drawn by
BranchKit; you can also click a file in it, or close it with its close
button. Your answer is remembered: in that project, the name means that
file from now on. Answers are records in the `plugin.address.learned`
collection, where you can see them and delete one to be asked again.

A name shared by more than nine files is not a name; say more of its path.

## Setting up the Claude Code hook

The plugin does not read your disk. Claude Code tells it which project you
are in: the plugin's own binary, run as `address-plugin claude-hook`, is a
Claude Code hook that lists the project's files with `git ls-files` and sends
them to the plugin over the socket it listens on (its port and pairing token
are in `connect.json` beside the binary). Add it to `~/.claude/settings.json`.
The plugin's **Claude Code** settings tab shows these hooks with the path of
your installation filled in, ready to copy, and whether Claude Code has
reported a project yet. By hand, with the path to the plugin's folder in
your BranchKit installation:

```json
{
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "command": "\"$HOME/Library/Application Support/BranchKit/plugins/address/address-plugin\" claude-hook", "timeout": 5 }] }
    ],
    "UserPromptSubmit": [
      { "hooks": [{ "type": "command", "command": "\"$HOME/Library/Application Support/BranchKit/plugins/address/address-plugin\" claude-hook", "timeout": 5 }] }
    ]
  }
}
```

It runs at the start of each session and before each prompt, so the names
follow the project of your latest prompt. It never delays or changes a
prompt: it prints nothing, gives up after three seconds, and always exits
successfully, reporting any problem on stderr. Paths are relative to the
folder Claude Code runs in, as Claude Code resolves `@` mentions. Files whose
path contains a space are left out, since a space ends a mention.

## Actions

| Action | Does |
|---|---|
| `address.mention` | Type the address of the file `name` names, or offer the choices when it names several |
| `address.insert` | Type the address of `path` |
| `address.browse` | List the files you changed or mentioned lately |
| `address.dismiss` | Close the choices |

## Collections

| Collection | Holds |
|---|---|
| `address_files` | One record per name you can say: `{spoken, paths, cwd}`. Several paths make the name a choice |
| `plugin.address.learned` | Your answers to "which file?": `{cwd, spoken, path}` |
| `plugin.address.choices` | While you choose: each badge and its file. Cleared when the hold ends |
| `plugin.address.choosing` | The exclusive mode that makes only the badges and "cancel" heard while you choose |

`address_files` is rebuilt from git on every report, so Settings shows it as
a one-line summary rather than an editable table: an edit there would be gone
at your next prompt. What you edit is `plugin.address.learned`, your answers.
A name another program writes to `address_files` still works until the next
report replaces the published names.

## Permissions

| Privilege | Why |
|---|---|
| `input` | Type the address where the cursor is |
| `locale` | Show when Claude Code last reported, in your own time zone and format |

It reads one other plugin's collection: the `alphabet`, for the badge words.

No network and no files. The one socket it listens on (`sockets.listen`) is
bound by BranchKit on the loopback interface and needs the pairing token.

## Platform support

Naming, choosing and typing work wherever BranchKit runs. The hook runs `git`
and connects to the loopback port in `connect.json`; it has been run on macOS.

## Build

Go, [plugin-sdk-go](https://github.com/branchkit/plugin-sdk-go).

```bash
cd src && go build -o ../address-plugin . && go test ./...
```

## License

MIT. See [LICENSE](LICENSE). `src/lexicon.txt.gz` is derived from the CMU
Pronouncing Dictionary, BSD-licensed by Carnegie Mellon University; its
notice is in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
