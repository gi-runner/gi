Feature: Token API

Scenario: issue a token
  * methodIs POST
  * pathMatches '/token'
  * def response = { access_token: 'tok' }

Scenario: read the caller
  * methodIs GET
  * pathMatches '/me'
  * def responseStatus = karate.eval("requestHeaders.Authorization && requestHeaders.Authorization[0] == 'Bearer tok' ? 200 : 401")
  * def response = { name: 'Ada' }
