Feature: Two load shapes, one file

# gi-load: name=soak vus=2 duration=3s
# gi-load: name=spike rate=5 duration=2s

Scenario: browse
  * assert true

@load=spike
Scenario: checkout
  * assert true
