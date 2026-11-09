Feature: Hello API

Scenario: greet
  * methodIs GET
  * pathMatches '/hello'
  * def response = { message: 'Hello, Ada!' }
