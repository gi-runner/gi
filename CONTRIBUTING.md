# Contributing

Use the Go version declared in `go.mod`. Install golangci-lint v2.14.0, then run:

```bash
make ci
```

This checks formatting, lint, vet, race tests, path/XML fuzzing and a no-CGO build. Browser tests use installed Chrome and skip when it is unavailable.

## Documentation

Keep reference pages alongside behavior changes. Preview the documentation from the repository root:

```bash
python3 -m venv .venv
.venv/bin/pip install -r docs/requirements.txt
.venv/bin/mkdocs serve
```

Before submitting, run `.venv/bin/mkdocs build --strict`.

CI runs Go checks on Linux, macOS and Windows. The Docs workflow checks pull requests and deploys the site from `main`. In repository settings, select **GitHub Actions** as the GitHub Pages source before the first deployment.
