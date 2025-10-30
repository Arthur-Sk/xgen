### xgen contribution notes (Go generator, XSD restrictions, and validation)

This document summarizes the recent changes and how the Go generator now handles XSD restrictions, missing types, and validation attributes.

---

### What was added/changed

- XSD restriction parsing was extended and wired into the Go generator. Supported simpleType facets:
  - pattern (xmlPattern.go)
  - enumeration (xmlEnumeration.go)
  - length, minLength, maxLength (xmlLength.go)
  - minInclusive/maxInclusive (xmlMaxInclusive.go, xmlMinInclusive.go)
  - minExclusive/maxExclusive (xmlMaxExclusive.go, xmlMinExclusive.go)
  - totalDigits (xmlTotalDigits.go) is parsed but currently not enforced (placeholder)

- The generator now emits validation in two ways:
  1) Validate() methods for named simple types and complex types whose fields use inline restrictions.
  2) go-playground validator tags on struct fields (preferred by the user) that mirror XSD restrictions.

- Missing type handling
  - Generation now runs in passes to ensure all named simple types appear before they are referenced:
    - First pass: emit all named simple types (simpleType with name).
    - Second pass: emit complex types, elements, attributes, etc.
    - Final sweep: catch any missed named simple types and emit them.
  - Cross-file references (xs:include) can still produce missing identifiers. To mitigate:
    - When generating common_types.xsd (out/commonTypes.go), a safety-net step declares aliases for any referenced T*-prefixed Go types that weren’t emitted (as `type TWhatever string`). This avoids undefined identifiers when common types refer to other common types that were not materialized yet.
    - When generating train_operation.xsd (out/trainOperation.go), if union members `tSendingType` / `tSendingTypeSpecial` are referenced but not emitted in that file, the generator synthesizes simple `int` aliases `TSendingType` and `TSendingTypeSpecial` to avoid undefined references.
  - In normal cases, the generator tries to avoid duplicates across files and only emits simple types it can find in the current file’s ProtoTree.

- Validator tag generation and Validate() behavior
  - Implemented in genGo.go via `buildValidateTag(base, r, optional, isSlice)` for go-playground, and `generateSimpleTypeValidator`/`generateComplexTypeValidator` for built-in methods.
  - Applied to complex type attributes/elements and to generated fields where restrictions are known.
  - Mapping:
    - string types:
      - length -> `validate:"len=N"` and same check in Validate()
      - minLength/maxLength -> `validate:"min=N,max=M"` (combined when both present) and same checks in Validate()
      - pattern -> `validate:"matches=^<regex>$"` (anchors added) AND Validate() also anchors the XSD regex with `^` and `$` so it matches the entire string. This fixes prior behavior where unanchored regexes could match substrings.
      - enumeration -> `validate:"oneof=v1 v2 v3"` (only when values have no spaces) and a map-based check in Validate().
    - numeric types (int/uint/float):
      - minInclusive -> `gte=`; maxInclusive -> `lte=`; minExclusive -> `gt=`; maxExclusive -> `lt=` with equivalent checks in Validate().
    - optionals -> prepend `omitempty`
    - slices -> prepend `dive` to apply rules per element
  - Note: tags are generated; running validation requires integrating go-playground/validator in consumer code (not in this repo). The `go.mod` includes validator dependency as indirect for reference.

- Inline restrictions for attributes/elements (complexType fields) are propagated to the generated validators and tags.

---

### Key files touched

- genGo.go
  - Added pre-pass and final sweep for named simple types.
  - Added validator tag generation (`buildValidateTag`), and applied tags for attributes/elements.
  - Added `generateSimpleTypeValidator`, `generateComplexTypeValidator`, and helper checks for numeric/string.
  - Added cross-file fallback helpers:
    - `ensureNamedType` now avoids broad duplication across files and only synthesizes local aliases in controlled cases (see Missing type handling above).
    - `ensureReferencedTypesDeclared()` as a safety net for common types output.

- parser.go / xml*.go
  - Ensured named simpleTypes are persisted on EndSimpleType; inline anonymous simpleTypes for elements/attributes apply base + restrictions back to the element/attribute.
  - Restriction handlers record limits in SimpleType.Restriction and defer application to EndRestriction/EndSimpleType.

---

### How to regenerate

Use Justfile targets:

- `just xsd-gen` (generates out/commonTypes.go and out/trainOperation.go)
- `go build ./...` to compile everything (including the generated code)

The generator runs twice (once per schema) and writes two Go files in the `out/` package.

---

### Validating at runtime (consumer code)

Validator tags are present on generated fields. To validate with go-playground/validator:

- In your application, import `github.com/go-playground/validator/v10`
- Instantiate a validator and call `validator.Struct(instance)` to validate.
- The existing `Validate()` methods can also be used in addition to or instead of validator tags.

Example:

```go
v := validator.New()
if err := v.Struct(myOutStruct); err != nil {
    // handle validation errors
}
```

---

### Known limitations / notes

- Some fallback aliases (`type TSomething string` or `int`) may be generated when cross-file references aren’t fully resolvable in the current schema pass. These ensure the code compiles but may not carry the full set of restrictions. Where possible, prefer referencing the schema where those simpleTypes are defined so that full validators are generated.
- totalDigits facet is parsed but not enforced yet.
- The generator still emits Validate() methods; these are complementary to validator tags and useful when consumers do not integrate go-playground/validator.

---

### Adding support for more facets

- Extend Restriction in proto.go and implement facet handlers in corresponding xml*.go files.
- Update `buildValidateTag` and the `generate*Validator` helpers to add the new rule mapping to tags and methods.

---

### Quick code map

- Parsing stacks and core data: parser.go, proto.go, utils.go
- Facets: xmlPattern.go, xmlEnumeration.go, xmlLength.go, xmlMin*/xmlMax*, xmlTotalDigits.go
- Simple/Complex types: xmlSimpleType.go, xmlComplexType.go, xmlElement.go, xmlAttribute.go
- Code generation (Go): genGo.go
- CLI: cmd/xgen/xgen.go



---

### Update: Field type selection prefers named simpleTypes (2025-10-29)

- When generating Go fields for elements and attributes, the generator now prefers the referenced named XSD simpleType over its resolved base type. This ensures specific domain types appear in the output (e.g., crewStartTime becomes *TDateTime instead of *string).
- Fallback rules for determining the Go field type used in struct fields:
  1. If TypeRef points to a named simpleType present in the schema, use that named type directly (Go name, not pointer unless optional) and generate its Validate() method and validator mapping as usual.
  2. If TypeRef refers to a built-in XSD primitive (xs:*), map it to the Go built-in via BuildInTypes and use that.
  3. Otherwise, resolve via getBasefromSimpleType(TypeRef or Type) and map to the proper Go type.
- Optional fields get pointer types (for non-slices) and include `,omitempty` in their XML tags. Slices don’t get pointer element types.
- Validator tags still reflect the underlying base restrictions of the named type (pattern, enum, length, numeric bounds, etc.).
- This change fixes cases where specific types were lost, for example:
  - Driver.CrewStartTime: now `*TDateTime` instead of `*string`.
  - TOperation.OperationDate: now `*TDateTime` instead of `string`.
- Built-in mapping is applied in both attributes and elements to avoid emitting undefined CamelCase types like `NonNegativeInteger`; these are now mapped to proper Go primitives (`int`, etc.).


---

### Update: go-playground validator tags and regex escaping (2025-10-30)

Problem observed:
- go-playground validator did not execute custom rule(s) defined in struct tags like `validate:"regexpp=^\d{2}\.\d{2}\.\d{4}$"`.
- Root cause: reflect.StructTag requires values to be valid quoted Go string literals. Regex backslashes inside struct tag values must be double-escaped so that the literal remains valid and can be parsed, e.g. `\\d` and `\\.`.
- If the escaping is not correct, `StructTag.Get("validate")` returns an empty string and validator silently skips the field’s validations.

Example:
- Incorrect (unparsed by reflect, skipped):
  `validate:"regexpp=^([0-9]{2}\.[0-9]{2}\.[0-9]{4})$"`
- Correct (parsed; note the doubled backslashes):
  `validate:"regexpp=^([0-9]{2}\\.[0-9]{2}\\.[0-9]{4})$"`

What we changed temporarily:
- Manually fixed the tag on `out.Driver.CrewStartTime` to use doubled backslashes so that go-playground validator sees and enforces the rule.

What the generator should do:
- When emitting validator tags for `pattern` facets, double-escape backslashes so the struct tag is a valid Go string literal and `reflect.StructTag.Get("validate")` can read it.
- Continue anchoring pattern with `^` and `$` so patterns must match the whole value.

Using the custom rule at runtime:
- Register the custom rule once on your validator instance:

  ```go
  v := validator.New()
  _ = v.RegisterValidation("regexpp", func(fl validator.FieldLevel) bool {
      re, err := regexp.Compile(fl.Param())
      if err != nil { return false }
      val := fl.Field()
      if val.Kind() == reflect.Ptr { if val.IsNil() { return true }; val = val.Elem() }
      if val.Kind() != reflect.String { return false }
      return re.MatchString(val.String())
  })
  ```

Notes:
- You generally don’t need `RegisterCustomTypeFunc` for alias string types (e.g., `type TDateTime string`) because validator’s `Field()` exposes them with `Kind() == reflect.String`.
- Keep explicit `Validate()` methods as a fallback; they already anchor patterns.
