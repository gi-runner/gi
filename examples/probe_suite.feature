Feature: Probe Test Suite
  Comprehensive feature-by-feature verification before release.

  # gi-load: vus=5 duration=3s
  # gi-load: threshold p95 < 150ms
  # gi-load: threshold errorRate < 1%

  Background:
    * url baseUrl
    * header Accept = 'application/json'

  Scenario: 1. Health check & basic status
    * path 'health'
    * method get
    * status 200
    * match response == { ok: true }
    * match responseStatus == 200

  Scenario: 2. REST POST & Karate Fuzzy Matchers
    * def uniqueEmail = 'ada-' + uuid() + '@example.com'
    * path 'users'
    * request { name: 'Ada Lovelace', age: 36, email: '#(uniqueEmail)', tags: ['math', 'poet'] }
    * method post
    * status 201
    * match response ==
      """
      {
        id: '#uuid',
        name: '#regex Ada.*',
        age: '#number',
        email: '#string',
        tags: '#[2] #string',
        nickname: '##string'
      }
      """
    * match response.age == '#? _ >= 18'
    * match response.tags contains only ['poet', 'math']
    * def createdId = response.id

    # Read back by ID
    * path 'users', createdId
    * method get
    * status 200
    * match response.id == createdId
    * match response.name == 'Ada Lovelace'
    * match response contains { email: '#(uniqueEmail)' }

  Scenario: 3. Collections, JSONPath & Deep Contains
    * path 'users'
    * method get
    * status 200
    * match each response contains { id: '#uuid', name: '#string' }
    * def names = response[*].name
    * match names contains 'Ada Lovelace'
    * def adults = karate.jsonPath(response, "$[?(@.age > 30)]")
    * match adults == '#array'
    * match adults == '#? _.length > 0'

  Scenario: 4. Native XML DOM & XPath Queries and Mutations
    * def catalog = <catalog><book id="bk101"><title>Algorithms</title><price>49.95</price></book><book id="bk102"><title>Compilers</title><price>65.00</price></book></catalog>
    * match catalog /catalog/book[@id='bk101']/title == 'Algorithms'
    * match catalog count(/catalog/book) == 2
    * set catalog /catalog/book[@id='bk101']/price = '39.95'
    * match catalog /catalog/book[@id='bk101']/price == '39.95'
    * remove catalog /catalog/book[@id='bk102']
    * match catalog count(/catalog/book) == 1
    * xmlstring serialized = catalog
    * match serialized contains 'Algorithms'
    * match serialized !contains 'Compilers'

  Scenario: 5. Feature Call with Arguments and Scope
    * def res = call read('probe_child.feature') { name: 'Alan', count: 21 }
    * match res.greeting == 'Hello Alan'
    * match res.doubled == 42

  Scenario: 6. JS Collection Transformations
    * def raw = [1, 2, 3, 4, 5]
    * def evens = karate.filter(raw, function(x){ return x % 2 === 0 })
    * match evens == [2, 4]
    * def squares = karate.map(evens, function(x){ return x * x })
    * match squares == [4, 16]
    * def merged = karate.merge({ a: 1, b: 2 }, { b: 99, c: 3 })
    * match merged == { a: 1, b: 99, c: 3 }

  Scenario Outline: 7. Data-driven Outlines (<role>)
    * def payload = { role: '<role>', level: <level> }
    * match payload.level == <level>
    * match payload.role == '<role>'

    Examples:
      | role   | level |
      | admin  | 10    |
      | editor | 5     |
      | viewer | 1     |

  @fail
  Scenario: 8. Expected Failure with @fail tag
    * def value = 100
    * match value == 999

  @report=false
  Scenario: 9. Secret Masking with @report=false
    * def secretToken = 'super-secret-production-key-do-not-leak'
    * match secretToken == '#string'
