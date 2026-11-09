Feature: Hello

Scenario: the mock answers
  * def api = karate.start('hello.mock.feature')
  * url api.url
  * path 'hello'
  * method get
  * status 200
  * match response == { message: 'Hello, Ada!' }
