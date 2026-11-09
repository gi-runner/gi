Feature: Auth

Scenario: a bearer token reads the caller
  * def api = karate.start('auth.mock.feature')
  * url api.url
  * path 'token'
  * request {}
  * method post
  * status 200
  * def token = response.access_token
  * path 'me'
  * header Authorization = 'Bearer ' + token
  * method get
  * status 200
  * match response.name == 'Ada'
