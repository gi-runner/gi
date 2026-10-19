Feature: Users mock

  Scenario: reject a user without a name
    * methodIs POST
    * pathMatches '/users'
    * match request.name == '#notpresent'
    * def responseStatus = 400
    * def response = { error: 'name is required' }

  Scenario: create a user
    * methodIs POST
    * pathMatches '/users'
    * def id = uuid()
    * def tags = request.tags
    * def user = { id: id, name: request.name, age: request.age, email: request.email, tags: tags }
    * def saved = storeSet(id, user)
    * def responseStatus = 201
    * def response = user

  Scenario: read a user
    * methodIs GET
    * pathMatches '/users/{id}'
    * def user = storeGet(pathParams.id)
    * match user == '#notnull'
    * def responseStatus = 200
    * def response = user

  Scenario: list users
    * methodIs GET
    * pathMatches '/users'
    * def responseStatus = 200
    * def response = storeAll()
