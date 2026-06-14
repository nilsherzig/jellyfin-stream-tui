# jellyfin-stream-tui

Browse a Jellyfin server and play its media with mpv, right from the terminal.

## Features

- **Watch media.** Browse your libraries and play any movie or episode in mpv.
- **Resume.** Start where you left off. The home page lists what you are still
  watching, episodes show their series and number (`Dark → S01E01 Secrets`), and
  a badge marks progress (`[42%]`) or a finished item (`[✓]`).
- **Sync progress.** While you watch, the app reports your position back to
  Jellyfin. Other clients pick up where you stopped.

## Requirements

- Go 1.26+
- mpv

The flake and `shell.nix` provide both.

## Install

With Nix flakes:

```sh
nix run github:nilsherzig/jellyfin-stream-tui   # run once
nix profile install github:nilsherzig/jellyfin-stream-tui   # install
```

The package wraps mpv onto the binary's PATH, so mpv ships as a runtime
dependency.

From source:

```sh
go build -o jellyfin-stream-tui ./cmd/jellyfin-stream-tui
```

## Configure

Create `config.yaml` and edit it:

```yaml
server: https://jellyfin.example.com
username: your-user
password: your-password
```

## Run

```sh
jellyfin-stream-tui                 # reads ./config.yaml
jellyfin-stream-tui -config path.yaml
```

## Keys

| Key              | Action                  |
|------------------|-------------------------|
| `↑`/`k`, `↓`/`j` | move                    |
| `⏎`/`l`/`→`      | open folder or play     |
| `esc`/`h`/`←`    | go back                 |
| `q`/`ctrl+c`     | quit                    |

Navigation walks `ParentId` uniformly: library → series → season → episode, or
library → movie.

Pass extra mpv flags through `JFTUI_MPV_ARGS`, for example
`JFTUI_MPV_ARGS="--sub-file=subs.srt"`.

## Develop

```sh
nix develop      # go + mpv
go test ./...
```

## Layout

| Package             | Job                                                       |
|---------------------|-----------------------------------------------------------|
| `internal/config`   | load and validate the YAML config                         |
| `internal/jellyfin` | API client: login, browse, stream URL, playback reporting |
| `internal/player`   | run mpv and read the position over its JSON IPC socket    |
| `internal/tui`      | Bubble Tea model: navigation and rendering                |

Two details worth knowing:

- Progress reporting needs one `PlaySessionId` across start, progress, and stop.
  Without it Jellyfin drops the position. Jellyfin also saves a resume point only
  past roughly 5% of the runtime.
- The player polls mpv's IPC socket. It ignores event lines and reads only
  command replies (the ones with an `error` field).

`_smoke/` holds a manual end-to-end test against a real server. The Go build
ignores it. Run it headless:

```sh
JFTUI_MPV_ARGS="--vo=null --no-video --ao=null --length=12" go run ./_smoke
```
