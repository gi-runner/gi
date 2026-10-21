# Data-driven tests and files

Use a [Scenario Outline](#scenario-outline) to repeat a test with different inputs. Use `read()` to load a fixture, or `json`, `yaml`, `csv` and `xml` to parse data written inside a feature.

## Read a fixture

Save `{ "name": "Ada", "age": 36 }` as `user.json` beside your feature. Inside a scenario:

```gherkin
* def user = read('user.json')
* match user.name == 'Ada'
```

The file extension determines the format. Relative paths start at the feature directory; see [file paths](language.md#read).

## Inline YAML and CSV

Three double quotes enclose multiline data. The parser converts it to a value you can check with `match`:

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

YAML preserves numbers. CSV cells are strings, so the example checks age against `'36'`. CSV accepts a leading byte-order mark and rows of different lengths.

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
* def name = gi.xmlPath(user, '/user/name')
* def names = gi.jsonPath({ users: [{ name: 'Ada' }] }, '$..name')
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

A table runs the same steps with different inputs. Save this as `ages.feature`:

```gherkin
Feature: Age checks

Scenario Outline: minimum age
  * def accepted = <age> >= 18
  * match accepted == <expected>
  Examples:
    | age | expected |
    | 17  | false    |
    | 18  | true     |
    | 36  | true     |
```

Run `gi test ages.feature`. Three scenarios pass, one per row. Change the first expected value to `true` to see a failure for that row.

`<name>` in steps, docstrings and tables is replaced before execution. Each column is also a variable; ordinary table cells bind strings. Use the placeholder form above to insert a numeric or boolean literal into an expression, or a typed `!` column as described below.

The scenario name gains the row, for example `minimum age [age=17, expected=false]`. `__row` is the row object and `__num` is the zero-based row number. Tags on the `Examples` block are added to its scenarios.

## Dynamic Examples

A one-column header that contains `(`, or a single-column Examples table with no body rows, is an expression. gi evaluates it when the file runs, after config is loaded. The expression must return a list of objects. Each object is a row, and its keys are the placeholders.

Do not add data rows under the expression; its returned list supplies them. Older features with a dummy row are still accepted.

```gherkin
@setup
Scenario:
  * def data = [{ name: 'Ada' }, { name: 'Grace' }]

Scenario Outline: hello <name>
  * match name == '#string'
  * match name == __row.name
  Examples:
    | gi.setup().data |
```

`gi.setupOnce().data` computes the setup data once for the run. The expression can be any JavaScript that returns the list, including `read('rows.json')`.

An outline with a dynamic header produces no scenarios when the list is empty.

Columns ending in `!` are evaluated as expressions and bound without the suffix. For example, `count!` containing `2` binds the number `2`, while an ordinary `count` column binds a string. Dynamic rows retain nested objects and lists. Excluded outlines are filtered before their expressions execute.

## Common mistakes

| Symptom | Check |
| --- | --- |
| A CSV or Examples value does not match `#number` | CSV cells and ordinary Examples columns are strings. Convert with `Number(value)` or use a typed `!` Examples column. |
| A dynamic outline runs no rows | Its expression must return a list of objects. An empty list runs no scenarios. |
| A data file cannot be found | Relative paths start at the feature directory, then the project root. See [read](language.md#read). |
| XML changes are lost or rejected | Use XPath `set`/`remove`; the JavaScript object view is read-only. |
