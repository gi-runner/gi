# Install

Homebrew, then `gi demo`.

```bash
brew install gi-runner/tap/gi
gi demo
```

That taps `gi-runner/homebrew-tap` and installs the release binary. No Go toolchain is required. Linux Homebrew uses the same command.

Without Homebrew:

```bash
curl -fsSL https://raw.githubusercontent.com/gi-runner/gi/main/install.sh | sh
gi demo
```

The script unpacks a release into `/usr/local/bin`. Set `GI_INSTALL_DIR` to choose another directory. It builds with Go, which needs [Go](https://go.dev/dl/) 1.27.2 or newer, only when the release archive is missing.

From a checkout of this repository, `make build` writes `bin/gi`, and `./bin/gi demo` is the same first run.

Install [Go](https://go.dev/dl/) 1.27.2 or newer when you want the module directly:

```bash
go install github.com/gi-runner/gi/cmd/gi@latest
```

Check the installation:

```bash
gi version
gi test --help
```

## From a release

Each tagged release publishes archives for Linux, macOS, and Windows on amd64 and arm64 with a `checksums.txt`, and an image at `ghcr.io/gi-runner/gi`. The image holds only the binary, so it runs API, mock, and load tests; browser tests need Chrome on the host.

```bash
docker run --rm -v "$PWD:/work" ghcr.io/gi-runner/gi test features
```

In GitHub Actions:

```yaml
- uses: gi-runner/gi@v1
  with:
    args: test features --junit gi.xml
```

## Build from source

From a checkout:

```bash
go build -o bin/gi ./cmd/gi
./bin/gi version
```

Follow the [quick start](quickstart.md). Multi-line shell examples use Bash; in PowerShell, join the lines and omit each trailing `\`.

`gi check features/` reports JVM-only calls and tags gi cannot run, without executing the files.

The [VS Code extension](https://github.com/gi-runner/gi/tree/main/editors/vscode) highlights a feature file, completes the short snippets, and runs the open file. Open that folder and use **Run Extension**.

GoLand and IntelliJ highlight the same files from `editors/idea/gi.tmbundle` (**Settings → Editor → TextMate Bundles**). A failed step prints `file:line`.

From a checkout, before a tagged release exists:

```bash
brew install --HEAD ./packaging/homebrew/gi.rb
```

## If installation fails

| Symptom | Check |
| --- | --- |
| `gi: command not found` | Add Go's executable directory to `PATH`: `GOBIN` when set, otherwise `bin` under `go env GOPATH`. Open a new terminal after changing it. |
| The Go version is too old | Check `go version`; building gi requires Go 1.27.2 or newer. |
| Browser tests cannot start | Install Chrome separately; API tests do not require it. |
