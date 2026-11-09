Feature: Outline

Scenario Outline: add two numbers
  * assert <a> + <b> == <sum>

  Examples:
    | a | b | sum |
    | 1 | 2 | 3   |
    | 4 | 5 | 9   |
