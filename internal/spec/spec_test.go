package spec

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gi-runner/gi"
	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/feature"
)

const reminderSpec = `# Feature Specification: Payment reminder

### User Story 1 - Reminder (Priority: P1)

A billing job reminds the contact once.

**Acceptance Scenarios**:

1. **Given** an invoice due 1 March and partly paid, **When** POST /invoices/{id}/reminders, **Then** status 201 and one reminder is sent
2. **Given** the same invoice was reminded, **When** POST /invoices/{id}/reminders, **Then** status 409

### Edge Cases

- The invoice id is unknown.

## Requirements

### Functional Requirements

- **FR-016**: System MUST send one reminder. [NEEDS CLARIFICATION: show the amount still due?]

## Success Criteria

### Measurable Outcomes

- **SC-001**: The reminder endpoint handles 50 concurrent users with p95 under 200ms.
`

const reminderContract = `openapi: 3.0.3
paths:
  /invoices/{id}/reminders:
    post:
      operationId: sendReminder
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
            example: inv_1
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/Reminder'
      responses:
        '201':
          description: Created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ReminderResult'
        '409':
          description: Already sent
components:
  schemas:
    Reminder:
      type: object
      required: [invoiceId]
      properties:
        invoiceId:
          type: string
          example: inv_1
    ReminderResult:
      type: object
      required: [sent, to]
      properties:
        sent:
          type: integer
        to:
          type: string
`

const reminderFeature = `# gi-spec: reminder
# gi-load: name=spec vus=50 duration=30s
# gi-load: threshold p95 < 200ms
@US1 @FR-016
Feature: Reminder

Background:
  * url baseUrl

# spec.md:9
Scenario: status 201 and one reminder is sent
  * path 'invoices', 'inv_1', 'reminders'
  * request { invoiceId: 'inv_1' }
  * method post
  * status 201
  * match response contains { sent: '#number', to: '#string' }

# spec.md:10
Scenario: status 409
  * path 'invoices', 'inv_1', 'reminders'
  * request { invoiceId: 'inv_1' }
  * method post
  * status 409
`

func TestSpecKitCompilesReminder(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "spec.md"), reminderSpec)
	writeFile(t, filepath.Join(dir, "contracts", "reminders.yaml"), reminderContract)
	writeFile(t, filepath.Join(dir, "features", "old.feature"), "# gi-spec: old\nFeature: Old\n\nScenario: gone\n  * method get\n")
	writeFile(t, filepath.Join(dir, "features", "hand.feature"), "Feature: Hand\n\nScenario: kept\n  * def n = 1\n")

	result, err := Compile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0].Scenarios != 2 || !result.Files[0].Load {
		t.Fatalf("files: %+v", result.Files)
	}
	got, err := os.ReadFile(result.Files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != reminderFeature {
		t.Fatalf("feature:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "features", "old.feature")); !os.IsNotExist(err) {
		t.Fatalf("owned feature still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "features", "hand.feature")); err != nil {
		t.Fatal(err)
	}
	parsed, err := feature.Parse(result.Files[0].Path, string(got))
	if err != nil {
		t.Fatal(err)
	}
	if findings := engine.Check(parsed, nil); len(findings) != 0 {
		t.Fatalf("check: %+v", findings)
	}
	if len(parsed.Scenarios) != 2 || !feature.HasTag(parsed.Scenarios[0].Tags, "FR-016") || !feature.HasTag(parsed.Scenarios[0].Tags, "US1") {
		t.Fatalf("tags: %+v", parsed.Scenarios[0].Tags)
	}
	if plans := parsed.Plans(); len(plans) != 1 || plans[0].Name != "spec" || plans[0].Load.VUs != 50 || len(plans[0].Load.Thresholds) != 1 {
		t.Fatalf("load: %+v", plans)
	}
	notes := make([]string, 0, len(result.Notes))
	for _, note := range result.Notes {
		notes = append(notes, note.Kind+": "+note.Text)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "edge: The invoice id is unknown.") || !strings.Contains(joined, "clarify: FR-016") {
		t.Fatalf("notes:\n%s", joined)
	}

	var n int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		call := n
		mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/invoices/inv_1/reminders" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"sent":1,"to":"ada@example.com"}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"sent":0,"to":"ada@example.com"}`))
	}))
	defer srv.Close()
	gi.Test(t, gi.Options{Vars: map[string]any{"baseUrl": srv.URL}, Jobs: 1}, result.Files[0].Path)
}

func TestAmbiguousScenarioIsNotGuessed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "spec.md"), `
# Feature Specification: Two calls

### User Story 1 - Both (Priority: P1)

**Acceptance Scenarios**:

1. **Given** a client, **When** the user asks, **Then** something happens
`)
	writeFile(t, filepath.Join(dir, "openapi.yaml"), `
openapi: 3.0.3
paths:
  /a:
    get:
      responses:
        '200': { description: ok }
  /b:
    post:
      responses:
        '201': { description: created }
`)
	result, err := Compile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 1 || result.Files[0].Scenarios != 2 {
		t.Fatalf("contract scenarios: %+v", result.Files)
	}
	text, err := os.ReadFile(result.Files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), "@contract") || strings.Contains(string(text), "something happens") {
		t.Fatalf("feature:\n%s", text)
	}
	var skipped bool
	for _, note := range result.Notes {
		if note.Kind == "skipped" && strings.Contains(note.Text, "POST /orders/{id}") {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("notes: %+v", result.Notes)
	}
}

func TestOneOperationBindsProse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "spec.md"), `
### User Story 1 - Read (Priority: P1)

**Acceptance Scenarios**:

1. **Given** a book, **When** the client asks for it, **Then** status 200
`)
	writeFile(t, filepath.Join(dir, "openapi.yaml"), `
openapi: 3.0.3
paths:
  /books/{id}:
    get:
      parameters:
        - { name: id, in: path, required: true, schema: { type: string, example: b1 } }
      responses:
        '200':
          description: ok
          content:
            application/json:
              schema: { type: object, required: [title], properties: { title: { type: string } } }
`)
	result, err := Compile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	text := readFile(t, result.Files[0].Path)
	if !strings.Contains(text, "* path 'books', 'b1'") || !strings.Contains(text, "* method get") || !strings.Contains(text, "* match response contains { title: '#string' }") {
		t.Fatalf("feature:\n%s", text)
	}
}

func TestKiroAndOpenSpec(t *testing.T) {
	kiro := t.TempDir()
	writeFile(t, filepath.Join(kiro, "requirements.md"), `
# Payment reminder

## Reminder

WHEN POST /invoices/{id}/reminders THE SYSTEM SHALL send one reminder
`)
	writeFile(t, filepath.Join(kiro, "openapi.yaml"), `
openapi: 3.0.3
paths:
  /invoices/{id}/reminders:
    post:
      parameters:
        - { name: id, in: path, required: true, schema: { type: string, example: inv_1 } }
      responses:
        '201': { description: Created }
`)
	kresult, err := Compile(kiro, "")
	if err != nil {
		t.Fatal(err)
	}
	ktext := readFile(t, kresult.Files[0].Path)
	if !strings.Contains(ktext, "Feature: Reminder") || !strings.Contains(ktext, "* method post") || !strings.Contains(ktext, "* status 201") {
		t.Fatalf("kiro:\n%s", ktext)
	}

	open := t.TempDir()
	writeFile(t, filepath.Join(open, "spec.md"), `
# Payment reminder

### Requirement: Payment reminder

#### Scenario: Partly paid invoice
- GIVEN an invoice due 1 March
- WHEN POST /invoices/{id}/reminders
- THEN one reminder is sent
`)
	writeFile(t, filepath.Join(open, "openapi.yaml"), `
openapi: 3.0.3
paths:
  /invoices/{id}/reminders:
    post:
      parameters:
        - { name: id, in: path, required: true, schema: { type: string, example: inv_1 } }
      responses:
        '200': { description: ok }
`)
	oresult, err := Compile(open, "")
	if err != nil {
		t.Fatal(err)
	}
	otext := readFile(t, oresult.Files[0].Path)
	if !strings.Contains(otext, "Scenario: Partly paid invoice") || !strings.Contains(otext, "* status 200") {
		t.Fatalf("openspec:\n%s", otext)
	}
}

func TestMissingExampleIsNotARequest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "spec.md"), `
### User Story 1 - Reminder (Priority: P1)

**Acceptance Scenarios**:

1. **Given** an invoice, **When** POST /invoices/{id}/reminders, **Then** status 201
`)
	writeFile(t, filepath.Join(dir, "openapi.yaml"), `
openapi: 3.0.3
paths:
  /invoices/{id}/reminders:
    post:
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [invoiceId]
              properties:
                invoiceId: { type: string }
      responses:
        '201': { description: Created }
`)
	result, err := Compile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Files) != 0 {
		t.Fatalf("files: %+v", result.Files)
	}
	notes := make([]string, 0, len(result.Notes))
	for _, note := range result.Notes {
		notes = append(notes, note.Kind+": "+note.Text)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "example:") || !strings.Contains(joined, "add example for id, invoiceId") {
		t.Fatalf("notes:\n%s", joined)
	}
}

func TestConcretePathIsTheExample(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "spec.md"), `
### User Story 1 - Reminder (Priority: P1)

**Acceptance Scenarios**:

1. **Given** an invoice, **When** POST /invoices/inv_9/reminders, **Then** status 201
`)
	writeFile(t, filepath.Join(dir, "openapi.yaml"), `
openapi: 3.0.3
paths:
  /invoices/{id}/reminders:
    post:
      responses:
        '201': { description: Created }
`)
	result, err := Compile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	text := readFile(t, result.Files[0].Path)
	if !strings.Contains(text, "* path 'invoices', 'inv_9', 'reminders'") || !strings.Contains(text, "# spec.md:") {
		t.Fatalf("feature:\n%s", text)
	}
}

func TestCredentialsAreNotInvented(t *testing.T) {
	t.Run("missing header", func(t *testing.T) {
		result := compileSpec(t, `
### User Story 1 - Read (Priority: P1)

**Acceptance Scenarios**:

1. **Given** a client, **When** GET /items, **Then** status 200
`, `
openapi: 3.0.3
paths:
  /items:
    get:
      parameters:
        - { name: X-Request-Id, in: header, required: true, schema: { type: string } }
      responses:
        '200': { description: ok }
`)
		if len(result.Files) != 0 || !noteHas(result, "add example for X-Request-Id") {
			t.Fatalf("files %+v notes %+v", result.Files, result.Notes)
		}
	})

	t.Run("bearer without a token", func(t *testing.T) {
		result := compileSpec(t, `
### User Story 1 - Admin (Priority: P1)

**Acceptance Scenarios**:

1. **Given** an operator, **When** GET /admin, **Then** status 200
`, bearerContract(""))
		if len(result.Files) != 0 || !noteHas(result, "add example for Authorization") {
			t.Fatalf("files %+v notes %+v", result.Files, result.Notes)
		}
	})

	t.Run("bearer example", func(t *testing.T) {
		result := compileSpec(t, `
### User Story 1 - Admin (Priority: P1)

**Acceptance Scenarios**:

1. **Given** an operator, **When** GET /admin, **Then** status 200
`, bearerContract("Bearer secret"))
		text := readFile(t, result.Files[0].Path)
		if !strings.Contains(text, "* header Authorization = 'Bearer secret'") {
			t.Fatalf("feature:\n%s", text)
		}
		var saw string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			saw = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		gi.Test(t, gi.Options{Vars: map[string]any{"baseUrl": srv.URL}}, result.Files[0].Path)
		if saw != "Bearer secret" {
			t.Fatalf("authorization %q", saw)
		}
	})

	t.Run("public alternative", func(t *testing.T) {
		result := compileSpec(t, `
### User Story 1 - Health (Priority: P1)

**Acceptance Scenarios**:

1. **Given** the service, **When** GET /health, **Then** status 200
`, `
openapi: 3.0.3
components:
  securitySchemes:
    bearerAuth: { type: http, scheme: bearer }
security:
  - {}
  - bearerAuth: []
paths:
  /health:
    get:
      responses:
        '200': { description: ok }
`)
		text := readFile(t, result.Files[0].Path)
		if strings.Contains(text, "Authorization") || !strings.Contains(text, "* method get") {
			t.Fatalf("feature:\n%s", text)
		}
	})

	t.Run("empty security overrides the document", func(t *testing.T) {
		result := compileSpec(t, `
### User Story 1 - Health (Priority: P1)

**Acceptance Scenarios**:

1. **Given** a client, **When** the client asks, **Then** status 200
`, `
openapi: 3.0.3
components:
  securitySchemes:
    bearerAuth: { type: http, scheme: bearer }
security:
  - bearerAuth: []
paths:
  /health:
    get:
      security: []
      responses:
        '200': { description: ok }
  /admin:
    get:
      responses:
        '200': { description: ok }
`)
		if len(result.Files) != 1 || result.Files[0].Scenarios != 1 || !noteHas(result, "add example for Authorization") {
			t.Fatalf("files %+v notes %+v", result.Files, result.Notes)
		}
		text := readFile(t, result.Files[0].Path)
		if strings.Contains(text, "Authorization") || !strings.Contains(text, "/health") && !strings.Contains(text, "'health'") {
			t.Fatalf("feature:\n%s", text)
		}
	})

	t.Run("cookie example", func(t *testing.T) {
		result := compileSpec(t, `
### User Story 1 - Session (Priority: P1)

**Acceptance Scenarios**:

1. **Given** a browser, **When** GET /session, **Then** status 200
`, `
openapi: 3.0.3
paths:
  /session:
    get:
      parameters:
        - { name: session, in: cookie, required: true, schema: { type: string, example: abc } }
      responses:
        '200': { description: ok }
`)
		text := readFile(t, result.Files[0].Path)
		if !strings.Contains(text, "* cookie session = 'abc'") {
			t.Fatalf("feature:\n%s", text)
		}
	})

	t.Run("api key", func(t *testing.T) {
		result := compileSpec(t, `
### User Story 1 - Read (Priority: P1)

**Acceptance Scenarios**:

1. **Given** a client, **When** GET /items, **Then** status 200
`, `
openapi: 3.0.3
components:
  securitySchemes:
    key: { type: apiKey, in: query, name: api_key }
security:
  - key: []
paths:
  /items:
    get:
      parameters:
        - { name: api_key, in: query, required: true, schema: { type: string, example: k } }
      responses:
        '200': { description: ok }
`)
		text := readFile(t, result.Files[0].Path)
		if !strings.Contains(text, "* param api_key = 'k'") {
			t.Fatalf("feature:\n%s", text)
		}
	})

	t.Run("oauth2 without a token", func(t *testing.T) {
		result := compileSpec(t, `
### User Story 1 - Admin (Priority: P1)

**Acceptance Scenarios**:

1. **Given** an operator, **When** GET /admin, **Then** status 200
`, `
openapi: 3.0.3
components:
  securitySchemes:
    oauth: { type: oauth2 }
security:
  - oauth: []
paths:
  /admin:
    get:
      responses:
        '200': { description: ok }
`)
		if len(result.Files) != 0 || !noteHas(result, "add example for Authorization") {
			t.Fatalf("files %+v notes %+v", result.Files, result.Notes)
		}
	})
}

func bearerContract(token string) string {
	header := ""
	if token != "" {
		header = `
      parameters:
        - name: Authorization
          in: header
          required: true
          schema: { type: string, example: ` + quoteYAML(token) + ` }`
	}
	return `
openapi: 3.0.3
components:
  securitySchemes:
    bearerAuth: { type: http, scheme: bearer }
security:
  - bearerAuth: []
paths:
  /admin:
    get:` + header + `
      responses:
        '200': { description: ok }
`
}

func quoteYAML(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func compileSpec(t *testing.T, spec, contract string) Result {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "spec.md"), spec)
	writeFile(t, filepath.Join(dir, "openapi.yaml"), contract)
	result, err := Compile(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func noteHas(result Result, text string) bool {
	for _, note := range result.Notes {
		if strings.Contains(note.Text, text) {
			return true
		}
	}
	return false
}

func TestResponseExampleAndEnumArePinned(t *testing.T) {
	result := compileSpec(t, `
### User Story 1 - Pay (Priority: P1)

**Acceptance Scenarios**:

1. **Given** an open order, **When** the client pays it, **Then** status 200
`, `
openapi: 3.0.3
paths:
  /orders/{id}/pay:
    post:
      parameters:
        - name: id
          in: path
          required: true
          schema: { type: string, example: ord_1 }
      responses:
        '200':
          description: Paid
          content:
            application/json:
              schema:
                type: object
                required: [status, totalCents, code]
                properties:
                  status: { type: string, example: paid }
                  totalCents: { type: integer, example: 2500 }
                  code: { type: string, enum: [already_paid] }
`)
	if len(result.Files) != 1 {
		t.Fatalf("files: %+v", result.Files)
	}
	text, err := os.ReadFile(result.Files[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(text)
	if !strings.Contains(got, "status: 'paid'") || !strings.Contains(got, "totalCents: 2500") || !strings.Contains(got, "code: 'already_paid'") {
		t.Fatalf("feature:\n%s", got)
	}
	if strings.Contains(got, "status: '#string'") || strings.Contains(got, "totalCents: '#number'") {
		t.Fatalf("example stayed a type marker:\n%s", got)
	}
}

func TestAgentCommandsNameTheLoop(t *testing.T) {
	root := filepath.Join("..", "..", "sdd")
	for _, path := range []string{
		filepath.Join(root, "spec-kit", "commands", "spec.md"),
		filepath.Join(root, "kiro", "gi.md"),
		filepath.Join(root, "openspec", "gi", "SKILL.md"),
	} {
		text := readFile(t, path)
		if !strings.Contains(text, "gi spec --json") || !strings.Contains(text, "gi check") || !strings.Contains(text, "gi test") {
			t.Fatalf("%s missing the loop", path)
		}
	}
	kiro := strings.ReplaceAll(readFile(t, filepath.Join(root, "kiro", "gi.md")), "\r\n", "\n")
	if !strings.HasPrefix(kiro, "---\n") || !strings.Contains(kiro, "inclusion: auto") || !strings.Contains(kiro, "name: gi") {
		t.Fatalf("kiro steering:\n%s", kiro)
	}
	skill := strings.ReplaceAll(readFile(t, filepath.Join(root, "openspec", "gi", "SKILL.md")), "\r\n", "\n")
	if !strings.HasPrefix(skill, "---\n") || !strings.Contains(skill, "name: openspec-gi") {
		t.Fatalf("openspec skill:\n%s", skill)
	}
}

func TestHumanTimeIsNotALoadProfile(t *testing.T) {
	if lines := loadLines([]string{"Users can complete account creation in under 2 minutes"}); len(lines) != 0 {
		t.Fatalf("lines: %v", lines)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}
