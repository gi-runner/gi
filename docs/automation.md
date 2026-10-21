# CI and automation

Run a tagged suite, save reports, and let gi's exit status fail the job. Install gi as one native binary. Your API, brokers and Chrome must be available for tests that use them.

## GitHub Actions

This example tests an API at the repository variable `API_BASE_URL`. Commit your tests under `features/` and tag the scenarios to run with `@smoke`.

```yaml
name: API tests
on: [push, pull_request]
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - uses: actions/setup-go@v7
        with:
          go-version: '1.27.2'
          cache: false
      - run: go install github.com/gi-runner/gi/cmd/gi@latest
      - name: Run smoke tests
        env:
          BASE_URL: ${{ vars.API_BASE_URL }}
        run: >-
          gi test features/ --tag @smoke
          --var "baseUrl=$BASE_URL"
          --report report.html --junit junit.xml
      - uses: actions/upload-artifact@v6
        if: always()
        with:
          name: gi-reports
          path: |
            report.html
            junit.xml
```

A successful job uploads `gi-reports` containing the HTML and JUnit files. A failed scenario marks the test step as failed; the artifact upload still runs when reports were produced.

Pin the gi version you have validated. For development, `@latest` installs the latest tagged release; to test gi from its own checkout, use `go build -o bin/gi ./cmd/gi`.

## Environment configuration

Use `--env staging` to load `gi-config-staging.js` and select environment tags. Pass deployment-specific values with `--var`, as in the workflow above. See [Configuration](suite.md#config) for file precedence.

## Load gates

```bash
gi load features/search.feature --var baseUrl="$BASE_URL" \
  --rate 100 --duration 30s --max-inflight 200 \
  --threshold 'p95 < 200ms' --threshold 'errorRate < 1%' \
  --report load.html --json load.json
```

See [Load](load.md) for profile and metric definitions. Exit status is `0` for success, `1` for a failed run and `2` for invalid arguments. Upload reports even when the command fails; see [Reports](reports.md).

## Common mistakes

| Symptom | Check |
| --- | --- |
| `baseUrl` is empty | Set the repository variable `API_BASE_URL`, or change the workflow to the variable your project uses. |
| The run says no scenarios ran | Check the feature paths and `@smoke` tags. A selection with no runnable scenarios fails. |
| The API is unreachable in CI | A developer's `localhost` is not the CI runner. Start the service in the job or use a reachable deployment. |
| The job fails but no report was uploaded | Keep `if: always()`. Early argument/config/parse failures may happen before report generation. |
