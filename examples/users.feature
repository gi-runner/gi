Feature: Users API
  One feature file, two modes.
  gi test runs each scenario once.
  gi load runs the same scenarios under virtual users.

  # gi-load: vus=10 duration=10s
  # gi-load: threshold p95 < 500ms
  # gi-load: threshold errorRate < 1%

  Background:
    * url baseUrl
    * header Accept = 'application/json'
    * def email = 'ada-' + uuid() + '@example.com'

  Scenario: create a user and read it back
    # created user matches the schema, including an optional nickname
    * path 'users'
    * request { name: 'Ada Lovelace', age: 36, email: '#(email)', tags: ['math', 'poet'] }
    * method post
    * status 201
    * match response ==
      """
      {
        id: '#uuid',
        name: 'Ada Lovelace',
        age: 36,
        email: '#(email)',
        tags: '#[] #string',
        nickname: '##string'
      }
      """
    * match response.age == '#? _ > 18'
    * match response.tags contains only ['poet', 'math']
    * def id = response.id

    * path 'users', id
    * method get
    * status 200
    * match response.id == id
    * match response contains { name: 'Ada Lovelace', email: '#(email)' }
    * match response contains deep { name: 'Ada Lovelace', tags: ['math'] }

  Scenario: collection match
    * path 'users'
    * request { name: 'Grace Hopper', age: 85, email: 'grace-' + uuid() + '@example.com', tags: ['cobol'] }
    * method post
    * status 201
    * def created = response

    * path 'users'
    * method get
    * status 200
    * match each response contains { id: '#uuid', name: '#string' }
    * match response contains deep { id: '#(created.id)', name: 'Grace Hopper' }
    * def ids = response[*].id
    * match ids contains created.id

  Scenario Outline: reject a user without a name
    * path 'users'
    * request { age: <age> }
    * method post
    * status 400
    * match response.error == '#string'

    Examples:
      | age |
      | 10  |
      | 20  |
