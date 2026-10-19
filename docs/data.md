# Data

`json`, `yaml`, `csv`, and `xml` turn text into values you can `match`. A docstring is the document. An inline right-hand side is evaluated first, and a string result is then parsed.

```gherkin
* yaml user =
  """
  name: Ada
  age: 36
  """
* match user.age == 36

* csv rows =
  """
  name,age
  Ada,36
  """
* match rows[0].age == '36'
```

YAML numbers become integers when they are whole, so `36` matches `36`. CSV keeps every cell as a string, including numbers. A leading byte-order mark is stripped. Rows may differ in length.

## XML

`xml` parses a native document preserving namespace bindings, mixed content and sibling order. A read-only JavaScript view exposes the following object shape; the root name is its only key.

| XML | Value |
| --- | --- |
| Text only | A string. |
| Attribute `id="1"` | Key `@id`. |
| Text plus attributes or children | Key `_` holds the trimmed text. |
| Repeated child name | An array, in document order. |

The object view uses local names. XPath uses namespace prefixes from the document. XML-to-XML structural matching compares expanded namespace names and ordered content.

```gherkin
* xml user =
  """
  <user id="1"><name>Ada</name></user>
  """
* match user.user.name == 'Ada'
* match user.user['@id'] == '1'
* xmlstring out = user
* match out contains '<name>Ada</name>'
```

`xmlstring` serializes native documents without sorting their children. Legacy one-key object formatting sorts names. `json data = user` converts to the object view; this conversion loses namespace identity and mixed-content ordering.

```gherkin
* match user /user/name == 'Ada'
* set user /user/name = 'Grace'
* remove user /user/@id
* def name = karate.xmlPath(user, '/user/name')
* def names = karate.jsonPath({ users: [{ name: 'Ada' }] }, '$..name')
```

Use XPath set/remove for XML mutation. Scalar and native XML fragment replacement are supported. Simple missing element paths can be created. Path/value tables support scalar XML updates as shown below. Structured object/list insertion and arbitrary complex missing paths remain incomplete. JSONPath supports recursive descent, filters, slices and unions within the native parser's grammar.

```gherkin
* xml user = { user: { name: 'Ada' } }
* set user /user
  | path | value |
  | name | 'Grace' |
* match user /user/name == 'Grace'
```

`xml name = object` builds a document from a one-root object. For XML table updates, cells are expressions; empty cells are skipped. Use scalar cell values; native XML fragment replacement is supported by direct XPath `set`.

## Scenario Outline

A table of literals is expanded when the file is parsed. `<name>` in the steps, docstrings, and tables is replaced.

```gherkin
Scenario Outline: age <age> is rejected
  * request { age: <age> }
  * method post
  * status 400
  Examples:
    | age |
    | 10  |
    | 20  |
```

The scenario name gains the row, for example `age 10 is rejected [age=10]`. Each column is also a variable, `__row` is the row object, and `__num` is the zero-based row number. Tags on the `Examples` block are added to those scenarios.

## Dynamic Examples

A one-column header that contains `(`, or a single-column Examples table with no body rows, is an expression. gi evaluates it when the file runs, after config is loaded. The expression must return a list of objects. Each object is a row, and its keys are the placeholders.

No body row is needed for the expression form. A legacy dummy body row is accepted and ignored.

```gherkin
@setup
Scenario:
  * def data = [{ name: 'Ada' }, { name: 'Grace' }]

Scenario Outline: hello <name>
  * match '<name>' == '<name>'
  Examples:
    | gi.setup().data |
    | |
```

`gi.setupOnce().data` is the same list, computed once per suite. The expression can be any JavaScript that returns the list, including `read('rows.json')`.

An outline with a dynamic header produces no scenarios when the list is empty.

Columns ending in `!` are evaluated as expressions and bound without the suffix. For example, `count!` containing `2` binds the number `2`, while an ordinary `count` column binds a string. Dynamic rows retain nested objects and lists. Excluded outlines are filtered before their expressions execute.
