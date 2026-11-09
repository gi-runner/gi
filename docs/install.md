# Install

The five-minute path is a binary, then `gi demo`.

```bash
curl -fsSL https://raw.githubusercontent.com/gi-runner/gi/main/install.sh | sh
gi demo
```

The script unpacks a release into `/usr/local/bin`. Until the first release is published it builds with Go instead, which needs [Go](https://go.dev/dl/) 1.27.2 or newer. Set `GI_INSTALL_DIR` to choose another directory.

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

The first command prints `gi` and the installed version. The second lists the functional test options.

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

Once `gi version` works, follow the [quick start](quickstart.md). Multi-line shell examples use Bash; in PowerShell, join the lines and omit each trailing `\`.

`gi check features/` reports JVM-only calls and tags gi cannot run, without executing the files.

The [VS Code extension](https://github.com/gi-runner/gi/tree/main/editors/vscode) highlights `* def` and `* match`, and runs the open feature file. Open `editors/vscode` in VS Code and use **Run Extension**.

GoLand and IntelliJ read the TextMate bundle at `editors/idea/gi.tmbundle`. In **Settings → Editor → TextMate Bundles**, add that directory. A failed step prints `file:line` in the terminal. That is the debugger for the first hour: the failing step, then `report.html`.

Homebrew, from a checkout, until the release tap exists:

```bash
brew install --HEAD ./packaging/homebrew/gi.rb
```

The release workflow publishes a cask when `HOMEBREW_TAP_TOKEN` is set. The cask repository is created by that publish.

## If installation fails

| Symptom | Check |
| --- | --- |
| `gi: command not found` | Add Go's executable directory to `PATH`: `GOBIN` when set, otherwise `bin` under `go env GOPATH`. Open a new terminal after changing it. |
| The Go version is too old | Check `go version`; building gi requires Go 1.27.2 or newer. |
| Browser tests cannot start | Install Chrome separately; API tests do not require it. |
