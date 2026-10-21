# Match assertions

Validate JSON, XML and scalar values with schema markers, predicates and structural assertions.

`match` compares two values. The left side is an expression. The right side is an expression, often a JSON document in a docstring. `#(name)` on the right is substituted before the comparison.

## Try it

Save as `assertions.feature`. This example needs no server:

```gherkin
Feature: Response validation

Scenario: validate one user
  * def user = { id: 42, name: 'Ada', age: 36 }
  * match user == { id: '#number', name: '#string', age: '#number' }
  * match user.age == '#? _ >= 18'
  * match user contains { name: 'Ada' }

Scenario: validate a collection
  * def users = [{ id: 42, name: 'Ada' }, { id: 43, name: 'Grace' }]
  * match each users contains { id: '#number', name: '#string' }
  * match users[*].id contains only [43, 42]
```

Run `gi test assertions.feature --report assertions.html`. Both scenarios pass. Change `age: 36` to `age: 17`: the predicate fails, the command exits `1`, and the report identifies the failed assertion.

## Operators

| Operator | Passes when |
| --- | --- |
| `==` | The two values are equal. |
| `!=` | They are not equal. |
| `contains` | The right side is found in the left. An object matches when every right-hand key is present and equal. A list matches when each right-hand element is present. A string matches when it is a substring. |
| `!contains` | `contains` would fail. |
| `contains deep` | Same as `contains`, and nested objects and lists are compared the same way. |
| `contains only` | Same elements, ignoring order. No extras. |
| `contains only deep` | `contains only`, applied to nested values. |
| `contains any` | At least one right-hand element is present. |
| `contains any deep` | `contains any`, applied to nested values. |
| `within` | The left value is contained by the right. |
| `!within` | `within` would fail. |

`each` walks a list. The rest of the match runs once per element. `_` is that element.

## Markers

A string on the right that starts with `#` is a marker, not a literal, except when the left side is that same string.

| Marker | Accepts |
| --- | --- |
| `#string` `#number` `#boolean` `#array` `#object` | That JSON type. `#number` is an integer or a fraction. |
| `#null` | JSON null. A missing key is not null. |
| `#notnull` | Anything except null. A missing key fails. |
| `#present` | The key exists, including when the value is null. |
| `#notpresent` | The key is absent. |
| `#uuid` | A canonical lowercase UUID string. |
| `#ignore` | Any value, including a missing key. |
| `#regex pattern` | A string matching the pattern. |
| `#? expression` | The expression is truthy. `_` is the value. `$` is the left-hand root of the match. |
| `##string` | The key is missing, null, or a string. The same optional form works for the other type markers. |
| `#[]` | An array. `#[] #string` is an array of strings. |
| `#[n]` | An array of length n. |

`#(name)` substitutes a variable. It is not a type marker.

`response.users[*].id` builds the list of `id` values. `[*]` is evaluated by gi, not by JavaScript.

A missing key fails `#string` and `#null`. Use `#notpresent` when the key must be absent, and `##string` when it may be absent.

Diffs stop after 20 mismatches. The failing path is included in the scenario error and in the HTML report.

## Common mistakes

| Symptom | Use instead |
| --- | --- |
| Equality fails because the response has extra keys | `contains` for a partial object; keep `==` when extra keys should fail. |
| An array has the right values in a different order | `contains only` for an unordered comparison. Duplicate counts still matter. |
| An optional field fails `#string` | `##string` accepts a missing or null field. |
| A missing field fails `#null` | `#notpresent` requires absence; `#null` requires an explicit null. |
| A predicate receives the wrong value | `_` is the value being checked; `$` is the left-hand root. |
