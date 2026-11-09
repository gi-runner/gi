package engine_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gi-runner/gi/internal/engine"
	"github.com/gi-runner/gi/internal/httpx"
	"github.com/gi-runner/gi/internal/mock"
)

func TestTagsAndTagValues(t *testing.T) {
	runRelease(t, "tags.feature", `@smoke @env=dev,staging @suite=x @suite=y
Feature: tags

@important @suite=a @suite=b
Scenario: merged
* match karate.tags contains 'smoke'
* match karate.tags contains 'important'
* match karate.tags contains 'env=dev,staging'
* match karate.tagValues.env == ['dev', 'staging']
* match karate.tagValues.smoke == []
* match karate.tagValues.suite == ['a', 'b']

Scenario: feature only
* match karate.tagValues.suite == ['x', 'y']
`, engine.Options{})
	runRelease(t, "untagged.feature", "Feature: none\nScenario: none\n* match karate.tags == []\n* match karate.scenarioOutline == null\n* match karate.scenario.exampleIndex == -1\n", engine.Options{})
}

func TestScenarioOutlineInfo(t *testing.T) {
	runRelease(t, "outline.feature", `Feature: outline

Scenario Outline: outline test
* def outline = karate.scenarioOutline
* match outline.name == 'outline test'
* match outline.sectionIndex == 0
* match outline.exampleTableCount == 1
* match outline.numScenariosToExecute == 2
* match karate.scenario.exampleData.name == name
* assert karate.scenario.exampleIndex >= 0

Examples:
| name   |
| first  |
| second |
`, engine.Options{})
}

func TestPrevRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"bar":"baz"}`))
	}))
	defer server.Close()
	runRelease(t, "prev.feature", `Feature: prev
Scenario: tracks the last request
* match karate.prevRequest == null
* url baseUrl
* path 'first'
* method get
* match karate.prevRequest.method == 'GET'
* match karate.prevRequest.url contains '/first'
* path 'users'
* request { name: 'Billie' }
* method post
* match karate.prevRequest.method == 'POST'
* match karate.prevRequest.url contains '/users'
* match karate.toString(karate.prevRequest.body) == '{"name":"Billie"}'
* match karate.response.status == 200
* match karate.response.body == { bar: 'baz' }
* match karate.response.header('content-type') contains 'application/json'
* match karate.request == null
`, engine.Options{HTTP: httpx.New(server.Client()), Vars: map[string]any{"baseUrl": server.URL}})
}

func TestTextAndDataHelpers(t *testing.T) {
	t.Setenv("GI_TEST_SYSENV", "set")
	runRelease(t, filepath.Join(t.TempDir(), "text.feature"), `Feature: text
Scenario: helpers
* match karate.extract('id=42;id=7', 'id=(\\d+)', 1) == '42'
* match karate.extractAll('id=42;id=7', 'id=(\\d+)', 1) == ['42', '7']
* match karate.extract('none', 'id=(\\d+)', 1) == null
* match karate.toCsv([{ name: 'John', age: 30 }, { name: 'Jane', age: 25 }]) == 'name,age\r\nJohn,30\r\nJane,25\r\n'
* match karate.toCsv([]) == ''
* match karate.fromCsv('name,age\nJohn,30\nJane,25\n') == [{ name: 'John', age: '30' }, { name: 'Jane', age: '25' }]
* match karate.fromCsv('') == []
* match karate.properties['GI_TEST_SYSENV'] == 'set'
* match karate.sysprop('GI_TEST_SYSENV') == 'set'
* match karate.sysprop('GI_TEST_MISSING', 'fallback') == 'fallback'
* match karate.properties['karate.env'] == 'qa'
* match karate.sysprop('karate.env') == 'qa'
* match karate.prettyXml('<a><b>1</b></a>') == '<a>\n  <b>1</b>\n</a>'
* match karate.sysenv('GI_TEST_SYSENV') == 'set'
* match karate.sysenv('GI_TEST_MISSING', 'fallback') == 'fallback'
* match karate.toAbsolutePath('data.json') == '#regex .+data\\.json'
* match karate.eval('1 + 2') == 3
* match karate.os.type == '#regex (windows|macosx|linux|unknown)'
* match karate.os.name == '#string'
* karate.setXml('doc', '<root><v>1</v></root>')
* match doc/root/v == '1'
* karate.setXml('doc', '/root/v', '<v>2</v>')
* match doc/root/v == '2'
* karate.logger.warn('logged', 1)
`, engine.Options{Env: "qa"})
}

func TestMatchHelper(t *testing.T) {
	runRelease(t, "match.feature", `Feature: match
Scenario: forms
* def foo = { a: 1 }
* match karate.match(foo, { a: 1 }) == { pass: true, message: null }
* match karate.match(foo, { a: 2 }).pass == false
* match karate.match('foo == { a: 1 }').pass == true
* match karate.match('foo contains { a: 2 }').pass == false
* match karate.match({ a: 'x' }, { a: '#string' }).pass == true
`, engine.Options{})
}

func TestExpect(t *testing.T) {
	runRelease(t, "expect.feature", `Feature: expect
Scenario: chains
* def o = { name: 'Billie', tags: ['a', 'b'], nested: { n: 1 } }
* karate.expect(o).to.have.property('name', 'Billie')
* karate.expect(o).to.have.property('name').and.equal('Billie')
* karate.expect(o).to.have.nested.property('nested.n', 1)
* karate.expect(o).to.have.all.keys(['name', 'tags'])
* karate.expect(o).to.have.keys(['name', 'tags', 'nested'])
* karate.expect(o.tags).to.include('a')
* karate.expect(o.tags).to.have.length(2)
* karate.expect(o).to.deep.include({ nested: { n: 1 } })
* karate.expect(5).to.be.above(4)
* karate.expect(5).to.be.below(6)
* karate.expect(5).to.be.within(1, 10)
* karate.expect(1.01).to.be.closeTo(1, 0.1)
* karate.expect(5).to.be.at.least(5)
* karate.expect('abc').to.be.a('string')
* karate.expect([]).to.be.an('array')
* karate.expect([]).to.be.empty
* karate.expect(true).to.be.true
* karate.expect(null).to.be.null
* karate.expect(1).to.be.ok
* karate.expect('x').to.exist
* karate.expect('b').to.be.oneOf(['a', 'b'])
* karate.expect('hello').to.match(/ell/)
* karate.expect(1).to.not.equal(2)
* karate.expect(1).not.to.equal(2)
* karate.expect(o).to.not.have.property('missing')
* def failed = function(){ try { karate.expect(1).to.equal(2); return false } catch (e) { return true } }
* assert failed()
`, engine.Options{})
}

func TestFaker(t *testing.T) {
	runRelease(t, "faker.feature", `Feature: faker
Scenario: values
* match karate.faker.timestamp() == '#number'
* match karate.faker.isoTimestamp() == '#regex \\d{4}-\\d{2}-\\d{2}T.*Z'
* match karate.faker.fullName() contains ' '
* match karate.faker.email() == '#regex .+@.+\\..+'
* def n = karate.faker.randomInt(10, 20)
* assert n >= 10 && n <= 20
* def m = karate.faker.randomInt()
* assert m >= 0 && m <= 1000
* match karate.faker.randomBoolean() == '#boolean'
* match karate.faker.alphanumeric(8) == '#regex [A-Za-z0-9]{8}'
* def body = { id: '#(karate.uuid())', name: '#(karate.faker.fullName())' }
* match body.id == '#uuid'
`, engine.Options{})
}

func TestExecAndFork(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	runRelease(t, "process.feature", `Feature: process
Scenario: exec
* match karate.exec('echo hello') contains 'hello'
* match karate.exec(['echo', 'hello', 'world']) contains 'hello world'
* match karate.exec({ line: 'sh -c "echo $MY_VAR"', env: { MY_VAR: 'test-value' } }) contains 'test-value'

Scenario: fork
* def proc = karate.fork({ args: ['echo', 'forked'] })
* proc.waitSync()
* match proc.stdOut contains 'forked'
* match proc.exitCode == 0
* def later = karate.fork({ args: ['echo', 'deferred'], start: false })
* later.start()
* later.waitSync()
* match later.stdOut contains 'deferred'

Scenario: output and listeners
* def seen = []
* def proc = karate.fork({ args: ['sh', '-c', 'echo one; sleep 0.2; echo ready'], listener: function(l){ seen.push(l) } })
* def line = proc.waitForOutput(function(l){ return l.indexOf('ready') >= 0 }, 5000)
* match line == 'ready'
* match seen contains 'one'
* def sleeper = karate.fork({ args: ['sleep', '60'] })
* match sleeper.alive == true
* sleeper.close()
* match sleeper.alive == false
`, engine.Options{})
}

func TestStartMock(t *testing.T) {
	dir := t.TempDir()
	mockFile := `Feature: mock
Background:
* configure cors = true
* configure responseHeaders = { 'X-Mock': 'yes' }

Scenario: pathMatches('/hello')
* def response = { hello: greeting }
`
	if err := os.WriteFile(filepath.Join(dir, "api-mock.feature"), []byte(mockFile), 0o600); err != nil {
		t.Fatal(err)
	}
	runRelease(t, filepath.Join(dir, "main.feature"), `Feature: start
Scenario: serves until the scenario ends
* def server = karate.start({ mock: 'api-mock.feature', arg: { greeting: 'world' }, pathPrefix: '/api' })
* url server.url
* path 'api', 'hello'
* method get
* status 200
* match response == { hello: 'world' }
* match responseHeaders['X-Mock'][0] == 'yes'
* match responseHeaders['Access-Control-Allow-Origin'][0] == '*'
* method options
* status 200
* match responseHeaders['Access-Control-Allow-Methods'][0] contains 'GET'
`, engine.Options{HTTP: httpx.New(nil), StartMock: mock.Start})
}

func TestProcessIsUnavailableInMocks(t *testing.T) {
	dir := t.TempDir()
	mockFile := "Feature: mock\nScenario: pathMatches('/run')\n* def response = karate.exec('echo hi')\n"
	if err := os.WriteFile(filepath.Join(dir, "m.feature"), []byte(mockFile), 0o600); err != nil {
		t.Fatal(err)
	}
	runRelease(t, filepath.Join(dir, "main.feature"), `Feature: guard
Scenario: exec in a mock fails
* def server = karate.start('m.feature')
* url server.url
* path 'run'
* method get
* status 500
* match response.error contains 'not available in a mock'
`, engine.Options{HTTP: httpx.New(nil), StartMock: mock.Start})
}
