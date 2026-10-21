# Install

Go 1.27.2 or newer.

```bash
go install github.com/gi-runner/gi/cmd/gi@latest
```

Check the installation:

```bash
gi version
gi test --help
```

The first command prints `gi` and the installed version. The second lists the functional test options.

## Build from source

From a checkout:

```bash
go build -o bin/gi ./cmd/gi
./bin/gi version
```

The first run is the [quick start](quickstart.md).

## If installation fails

| Symptom | Check |
| --- | --- |
| `gi: command not found` | Add Go's executable directory to `PATH`: `GOBIN` when set, otherwise `bin` under `go env GOPATH`. Open a new terminal after changing it. |
| The Go version is too old | Check `go version`; building gi requires Go 1.27.2 or newer. |
| Browser tests cannot start | Install Chrome separately; API tests do not require it. |
