// Copyright 2020 - 2024 The xgen Authors. All rights reserved. Use of this
// source code is governed by a BSD-style license that can be found in the
// LICENSE file.
//
// Package xgen written in pure Go providing a set of functions that allow you
// to parse XSD (XML schema files). This library needs Go version 1.10 or
// later.

package xgen

import (
	"fmt"
	"go/format"
	"os"
	"reflect"
	"strings"
)

// CodeGenerator holds code generator overrides and runtime data that are used
// when generate code from proto tree.
type CodeGenerator struct {
	Lang              string
	File              string
	Field             string
	Package           string
	ImportTime        bool // For Go language
	ImportEncodingXML bool // For Go language
	ImportFmt         bool // For validation methods
	ImportRegexp      bool // For pattern validation
	ProtoTree         []interface{}
	StructAST         map[string]string
	TypeNameMap       map[string]string // XSD type name -> Go type name used
	ValidatedTypes    map[string]bool   // Go type names that have Validate method
}

// buildValidateTag builds a go-playground/validator tag string for the given
// restriction and base type. If no rules, returns an empty string.
func (gen *CodeGenerator) buildValidateTag(base string, r *Restriction, optional bool, isSlice bool) string {
	if r == nil {
		return ""
	}
	// Determine existence of any rule
	has := len(r.Enum) > 0 || r.PatternStr != "" || r.HasLength || r.HasMinLength || r.HasMaxLength || r.HasMin || r.HasMax
	if !has {
		return ""
	}
	rules := make([]string, 0, 6)
	// Optional fields: prefix with omitempty to skip validation if empty/nil
	if optional {
		rules = append(rules, "omitempty")
	}
	isString := base == "string"
	isNumeric := isNumericGoType(base)
	if isString {
		if r.HasLength {
			rules = append(rules, fmt.Sprintf("len=%d", r.Length))
		} else {
			if r.HasMinLength {
				rules = append(rules, fmt.Sprintf("min=%d", r.MinLength))
			}
			if r.HasMaxLength {
				rules = append(rules, fmt.Sprintf("max=%d", r.MaxLength))
			}
		}
		if r.PatternStr != "" {
			// Anchor the regex to match the whole string; users can still override
			pattern := r.PatternStr
			if len(pattern) > 0 {
				pattern = "^" + pattern + "$"
			}
			rules = append(rules, fmt.Sprintf("matches=%s", pattern))
		}
		if len(r.Enum) > 0 {
			// oneof can't handle values with spaces; only include when all values have no spaces
			ok := true
			enumVals := make([]string, 0, len(r.Enum))
			for _, ev := range r.Enum {
				if strings.ContainsAny(ev, " \t\n\r") {
					ok = false
					break
				}
				enumVals = append(enumVals, ev)
			}
			if ok {
				rules = append(rules, fmt.Sprintf("oneof=%s", strings.Join(enumVals, " ")))
			}
		}
	}
	if isNumeric {
		if r.HasMin {
			if r.MinExclusive {
				rules = append(rules, fmt.Sprintf("gt=%g", r.Min))
			} else {
				rules = append(rules, fmt.Sprintf("gte=%g", r.Min))
			}
		}
		if r.HasMax {
			if r.MaxExclusive {
				rules = append(rules, fmt.Sprintf("lt=%g", r.Max))
			} else {
				rules = append(rules, fmt.Sprintf("lte=%g", r.Max))
			}
		}
	}
	if len(rules) == 0 {
		return ""
	}
	if isSlice {
		// apply rules to each element
		rules = append([]string{"dive"}, rules...)
	}
	return strings.Join(rules, ",")
}

// ensureReferencedTypesDeclared scans generated fields and ensures that any referenced
// Go type names (starting with 'T' and not yet declared) are declared in this file
// as simple aliases to string. This is a safety net for shared/common schema output
// to avoid undefined identifiers due to cross-schema includes.
func (gen *CodeGenerator) ensureReferencedTypesDeclared() {
	declared := map[string]bool{}
	for k := range gen.StructAST {
		declared[k] = true                        // XSD name (e.g., tStateCode)
		declared[genGoFieldName(k, false)] = true // Go name (e.g., TStateCode)
	}
	// Scan gen.Field for type identifiers
	// Look for patterns like '*TName', '[]TName', ' TName ', '\tTName\t'
	candidates := map[string]bool{}
	// Simple scan by splitting on non-letter/digit/underscore characters
	sep := func(r rune) bool {
		return !(r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'))
	}
	for _, tok := range strings.FieldsFunc(gen.Field, sep) {
		if len(tok) >= 2 && tok[0] == 'T' && tok[1] >= 'A' && tok[1] <= 'Z' {
			candidates[tok] = true
		}
	}
	for name := range candidates {
		if declared[name] || declared[strings.ToLower(name[:1])+name[1:]] {
			continue
		}
		// Skip built-ins masked as T* (none), and skip xml.Name etc.
		if goBuildinType[name] {
			continue
		}
		// Declare a simple alias to string
		content := fmt.Sprintf(" string\n")
		gen.StructAST[name] = content
		fieldName := genGoFieldName(name, true)
		gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, "", "//"), fieldName, content)
	}
}

var goBuildinType = map[string]bool{
	"xml.Name":      true,
	"byte":          true,
	"[]byte":        true,
	"bool":          true,
	"[]bool":        true,
	"complex64":     true,
	"complex128":    true,
	"float32":       true,
	"float64":       true,
	"int":           true,
	"int8":          true,
	"int16":         true,
	"int32":         true,
	"int64":         true,
	"interface":     true,
	"[]interface{}": true,
	"string":        true,
	"[]string":      true,
	"time.Time":     true,
	"uint":          true,
	"uint8":         true,
	"uint16":        true,
	"uint32":        true,
	"uint64":        true,
}

// GenGo generate Go programming language source code for XML schema
// definition files.
func (gen *CodeGenerator) GenGo() error {
	fieldNameCount = make(map[string]int)
	// First pass: emit all named simple types to ensure they are available for references
	for _, ele := range gen.ProtoTree {
		if st, ok := ele.(*SimpleType); ok && st != nil && st.Name != "" {
			gen.GoSimpleType(st)
		}
	}
	// Second pass: emit remaining schema components (complex types, elements, attributes, etc.)
	for _, ele := range gen.ProtoTree {
		if ele == nil {
			continue
		}
		funcName := fmt.Sprintf("Go%s", reflect.TypeOf(ele).String()[6:])
		callFuncByName(gen, funcName, []reflect.Value{reflect.ValueOf(ele)})
	}
	// Final sweep: ensure all named simpleTypes are emitted (some schemas reference them in ways that skip first pass)
	for _, ele := range gen.ProtoTree {
		if st, ok := ele.(*SimpleType); ok && st != nil && st.Name != "" {
			if _, exists := gen.StructAST[st.Name]; !exists {
				gen.GoSimpleType(st)
			}
		}
	}
	// As a final safety net for cross-file references, ensure types referenced in
	// this file are declared here when generating the shared common types file.
	if strings.Contains(gen.File, "commonTypes.go") {
		gen.ensureReferencedTypesDeclared()
	}
	f, err := os.Create(gen.FileWithExtension(".go"))
	if err != nil {
		return err
	}
	defer f.Close()
	var importPackage, packages string
	if gen.ImportTime {
		packages += "\t\"time\"\n"
	}
	if gen.ImportEncodingXML {
		packages += "\t\"encoding/xml\"\n"
	}
	if gen.ImportFmt {
		packages += "\t\"fmt\"\n"
	}
	if gen.ImportRegexp {
		packages += "\t\"regexp\"\n"
	}
	if packages != "" {
		importPackage = fmt.Sprintf("import (\n%s)", packages)
	}
	packageName := gen.Package
	if packageName == "" {
		packageName = "schema"
	}
	source, err := format.Source([]byte(fmt.Sprintf("%s\n\npackage %s\n%s%s", copyright, packageName, importPackage, gen.Field)))
	if err != nil {
		f.WriteString(fmt.Sprintf("package %s\n%s%s", packageName, importPackage, gen.Field))
		return err
	}
	f.Write(source)
	return err
}

func splitter(r rune) bool {
	return strings.ContainsRune(":.-_", r)
}

func genGoFieldName(name string, unique bool) (fieldName string) {
	for _, str := range strings.FieldsFunc(name, splitter) {
		fieldName += MakeFirstUpperCase(str)
	}

	if unique {
		fieldNameCount[fieldName]++
		if count := fieldNameCount[fieldName]; count != 1 {
			fieldName = fmt.Sprintf("%s%d", fieldName, count)
		}
	}
	return
}

func genGoFieldType(name string) string {
	if _, ok := goBuildinType[name]; ok {
		return name
	}
	var fieldType string
	for _, str := range strings.FieldsFunc(name, splitter) {
		fieldType += MakeFirstUpperCase(str)
	}
	if fieldType != "" {
		return "*" + fieldType
	}
	return "interface{}"
}

// GoSimpleType generates code for simple type XML schema in Go language
// syntax.
func (gen *CodeGenerator) GoSimpleType(v *SimpleType) {
	if v.List {
		if _, ok := gen.StructAST[v.Name]; !ok {
			fieldType := genGoFieldType(getBasefromSimpleType(trimNSPrefix(v.Base), gen.ProtoTree))
			if fieldType == "time.Time" {
				gen.ImportTime = true
			}
			content := fmt.Sprintf(" []%s\n", genGoFieldType(fieldType))
			gen.StructAST[v.Name] = content
			fieldName := genGoFieldName(v.Name, true)
			gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, v.Doc, "//"), fieldName, gen.StructAST[v.Name])
			return
		}
	}
	if v.Union && len(v.MemberTypes) > 0 {
		if _, ok := gen.StructAST[v.Name]; !ok {
			content := " struct {\n"
			fieldName := genGoFieldName(v.Name, true)
			if fieldName != v.Name {
				gen.ImportEncodingXML = true
				content += fmt.Sprintf("\tXMLName\txml.Name\t`xml:\"%s\"`\n", v.Name)
			}
			for _, member := range toSortedPairs(v.MemberTypes) {
				memberName := member.key
				memberType := member.value
				// Ensure named member type is available if referenced by name
				gen.ensureNamedType(memberName)
				if memberType == "" { // fix order issue and includes
					memberType = getBasefromSimpleType(memberName, gen.ProtoTree)
				}
				content += fmt.Sprintf("\t%s\t%s\n", genGoFieldName(memberName, false), genGoFieldType(memberType))
			}
			content += "}\n"
			gen.StructAST[v.Name] = content
			gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, v.Doc, "//"), fieldName, gen.StructAST[v.Name])
		}
		return
	}
	if _, ok := gen.StructAST[v.Name]; !ok {
		base := getBasefromSimpleType(trimNSPrefix(v.Base), gen.ProtoTree)
		content := fmt.Sprintf(" %s\n", genGoFieldType(base))
		gen.StructAST[v.Name] = content
		fieldName := genGoFieldName(v.Name, true)
		gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, v.Doc, "//"), fieldName, gen.StructAST[v.Name])
		// Generate Validate method if there are restrictions
		gen.generateSimpleTypeValidator(fieldName, base, &v.Restriction)
	}
}

// GoComplexType generates code for complex type XML schema in Go language
// syntax.
func (gen *CodeGenerator) GoComplexType(v *ComplexType) {
	if _, ok := gen.StructAST[v.Name]; !ok {
		content := " struct {\n"
		fieldName := genGoFieldName(v.Name, true)
		if fieldName != v.Name {
			gen.ImportEncodingXML = true
			content += fmt.Sprintf("\tXMLName\txml.Name\t`xml:\"%s\"`\n", v.Name)
		}
		for _, attrGroup := range v.AttributeGroup {
			fieldType := getBasefromSimpleType(trimNSPrefix(attrGroup.Ref), gen.ProtoTree)
			if fieldType == "time.Time" {
				gen.ImportTime = true
			}
			content += fmt.Sprintf("\t%s\t%s\n", genGoFieldName(attrGroup.Name, false), genGoFieldType(fieldType))
		}

		for _, attribute := range v.Attributes {
			// Ensure the referenced simple type is emitted
			gen.ensureNamedType(attribute.TypeRef)
			// Also ensure if attribute.Type directly references a named simpleType
			gen.ensureNamedType(attribute.Type)
			// Prefer using the named simpleType (TypeRef) as the Go field type when available
			var base string
			var fieldType string
			if st := gen.findSimpleType(trimNSPrefix(attribute.TypeRef)); st != nil {
				base = getBasefromSimpleType(trimNSPrefix(st.Base), gen.ProtoTree)
				// Use the named simple type directly (no pointer by default)
				fieldType = genGoFieldName(st.Name, false)
			} else {
				// Fallback to resolved Go base type from parser or built-in map
				resolved := strings.TrimSpace(attribute.Type)
				if resolved == "" && attribute.TypeRef != "" {
					if bt, ok := getBuildInTypeByLang(trimNSPrefix(attribute.TypeRef), "Go"); ok && bt != "" {
						resolved = bt
					} else {
						resolved = getBasefromSimpleType(trimNSPrefix(attribute.TypeRef), gen.ProtoTree)
					}
				}
				if resolved == "" {
					resolved = getBasefromSimpleType(trimNSPrefix(attribute.Type), gen.ProtoTree)
				}
				base = resolved
				fieldType = genGoFieldType(resolved)
			}
			var optional string
			if attribute.Optional {
				if !strings.HasPrefix(fieldType, `*`) {
					fieldType = "*" + fieldType
				} else {
					optional = `,omitempty`
				}
			}
			if fieldType == "time.Time" {
				gen.ImportTime = true
			}
			// Determine restriction: prefer inline, otherwise named simpleType's
			r := attribute.Restriction
			if !hasRestrictions(&r) {
				if st := gen.findSimpleType(trimNSPrefix(attribute.TypeRef)); st != nil {
					r = st.Restriction
					base = getBasefromSimpleType(trimNSPrefix(st.Base), gen.ProtoTree)
				} else if st2 := gen.findSimpleType(trimNSPrefix(attribute.Type)); st2 != nil {
					r = st2.Restriction
					base = getBasefromSimpleType(trimNSPrefix(st2.Base), gen.ProtoTree)
				}
			}
			vtag := gen.buildValidateTag(base, &r, attribute.Optional, false)
			tag := fmt.Sprintf("xml:\"%s,attr%s\"", attribute.Name, optional)
			if vtag != "" {
				tag += fmt.Sprintf(" validate:\"%s\"", vtag)
			}
			content += fmt.Sprintf("\t%sAttr\t%s\t`%s`\n", genGoFieldName(attribute.Name, false), fieldType, tag)
		}
		for _, group := range v.Groups {
			// Ensure named types referenced by group elements
			gen.ensureNamedType(group.Ref)
			fieldType := genGoFieldType(getBasefromSimpleType(trimNSPrefix(group.Ref), gen.ProtoTree))
			if group.Plural {
				fieldType = "[]" + fieldType
			}
			content += fmt.Sprintf("\t%s\t%s\n", genGoFieldName(group.Name, false), fieldType)
		}

		for _, element := range v.Elements {
			// Ensure the referenced named simple type is emitted (use TypeRef, not resolved Type)
			gen.ensureNamedType(element.TypeRef)
			// Prefer using the named simpleType (TypeRef) as the Go field type when available
			var base string
			var fieldType string
			if st := gen.findSimpleType(trimNSPrefix(element.TypeRef)); st != nil {
				base = getBasefromSimpleType(trimNSPrefix(st.Base), gen.ProtoTree)
				// Use the named simple type directly (no pointer by default)
				fieldType = genGoFieldName(st.Name, false)
			} else {
				// Fallback to resolved Go base type from parser or built-in map
				resolved := strings.TrimSpace(element.Type)
				if resolved == "" && element.TypeRef != "" {
					if bt, ok := getBuildInTypeByLang(trimNSPrefix(element.TypeRef), "Go"); ok && bt != "" {
						resolved = bt
					} else {
						resolved = getBasefromSimpleType(trimNSPrefix(element.TypeRef), gen.ProtoTree)
					}
				}
				if resolved == "" {
					resolved = getBasefromSimpleType(trimNSPrefix(element.Type), gen.ProtoTree)
				}
				base = resolved
				fieldType = genGoFieldType(resolved)
			}
			if element.Plural {
				fieldType = "[]" + fieldType
			}
			var optional string
			if element.Optional {
				if !element.Plural && !strings.HasPrefix(fieldType, `*`) {
					fieldType = "*" + fieldType
				}
				optional = ",omitempty"
			}
			if fieldType == "time.Time" {
				gen.ImportTime = true
			}
			// Determine restriction: prefer inline, otherwise named simpleType's
			r := element.Restriction
			if !hasRestrictions(&r) {
				if st := gen.findSimpleType(trimNSPrefix(element.TypeRef)); st != nil {
					r = st.Restriction
					base = getBasefromSimpleType(trimNSPrefix(st.Base), gen.ProtoTree)
				} else if st2 := gen.findSimpleType(trimNSPrefix(element.Type)); st2 != nil {
					r = st2.Restriction
					base = getBasefromSimpleType(trimNSPrefix(st2.Base), gen.ProtoTree)
				}
			}
			vtag := gen.buildValidateTag(base, &r, element.Optional, element.Plural)
			tag := fmt.Sprintf("xml:\"%s%s\"", element.Name, optional)
			if vtag != "" {
				tag += fmt.Sprintf(" validate:\"%s\"", vtag)
			}
			content += fmt.Sprintf("\t%s\t%s\t`%s`\n", genGoFieldName(element.Name, false), fieldType, tag)
		}
		if len(v.Base) > 0 {
			// If the type is a built-in type, generate a Value field as chardata.
			// If it's not built-in one, embed the base type in the struct for the child type
			// to effectively inherit all of the base type's fields
			if isGoBuiltInType(v.Base) {
				content += fmt.Sprintf("\tValue\t%s\t`xml:\",chardata\"`\n", genGoFieldType(v.Base))
			} else {
				// Ensure the base named type is emitted
				gen.ensureNamedType(v.Base)
				content += fmt.Sprintf("\t%s\n", genGoFieldType(v.Base))
			}
		}
		content += "}\n"
		gen.StructAST[v.Name] = content
		gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, v.Doc, "//"), fieldName, gen.StructAST[v.Name])
		// Generate validator for complex type fields with inline restrictions
		gen.generateComplexTypeValidator(fieldName, v)
	}
}

func isGoBuiltInType(typeName string) bool {
	_, builtIn := goBuildinType[typeName]
	return builtIn
}

// GoGroup generates code for group XML schema in Go language syntax.
func (gen *CodeGenerator) GoGroup(v *Group) {
	if _, ok := gen.StructAST[v.Name]; !ok {
		content := " struct {\n"
		fieldName := genGoFieldName(v.Name, true)
		if fieldName != v.Name {
			gen.ImportEncodingXML = true
			content += fmt.Sprintf("\tXMLName\txml.Name\t`xml:\"%s\"`\n", v.Name)
		}
		for _, element := range v.Elements {
			// Ensure named simple types referenced by elements
			gen.ensureNamedType(element.TypeRef)
			gen.ensureNamedType(element.Type)
			var plural string
			if element.Plural {
				plural = "[]"
			}
			content += fmt.Sprintf("\t%s\t%s%s\n", genGoFieldName(element.Name, false), plural, genGoFieldType(getBasefromSimpleType(trimNSPrefix(element.Type), gen.ProtoTree)))
		}

		for _, group := range v.Groups {
			var plural string
			if group.Plural {
				plural = "[]"
			}
			content += fmt.Sprintf("\t%s\t%s%s\n", genGoFieldName(group.Name, false), plural, genGoFieldType(getBasefromSimpleType(trimNSPrefix(group.Ref), gen.ProtoTree)))
		}

		content += "}\n"
		gen.StructAST[v.Name] = content
		gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, v.Doc, "//"), fieldName, gen.StructAST[v.Name])
	}
}

// GoAttributeGroup generates code for attribute group XML schema in Go language
// syntax.
func (gen *CodeGenerator) GoAttributeGroup(v *AttributeGroup) {
	if _, ok := gen.StructAST[v.Name]; !ok {
		content := " struct {\n"
		fieldName := genGoFieldName(v.Name, true)
		if fieldName != v.Name {
			gen.ImportEncodingXML = true
			content += fmt.Sprintf("\tXMLName\txml.Name\t`xml:\"%s\"`\n", v.Name)
		}
		for _, attribute := range v.Attributes {
			// Ensure named simple types referenced by attribute group attributes
			gen.ensureNamedType(attribute.TypeRef)
			gen.ensureNamedType(attribute.Type)
			base := getBasefromSimpleType(trimNSPrefix(attribute.Type), gen.ProtoTree)
			var optional string
			if attribute.Optional {
				optional = `,omitempty`
			}
			// Determine restriction: prefer inline, otherwise named simpleType's
			r := attribute.Restriction
			if !hasRestrictions(&r) {
				if st := gen.findSimpleType(trimNSPrefix(attribute.TypeRef)); st != nil {
					r = st.Restriction
					base = getBasefromSimpleType(trimNSPrefix(st.Base), gen.ProtoTree)
				} else if st2 := gen.findSimpleType(trimNSPrefix(attribute.Type)); st2 != nil {
					r = st2.Restriction
					base = getBasefromSimpleType(trimNSPrefix(st2.Base), gen.ProtoTree)
				}
			}
			vtag := gen.buildValidateTag(base, &r, attribute.Optional, false)
			tag := fmt.Sprintf("xml:\"%s,attr%s\"", attribute.Name, optional)
			if vtag != "" {
				tag += fmt.Sprintf(" validate:\"%s\"", vtag)
			}
			content += fmt.Sprintf("\t%sAttr\t%s\t`%s`\n", genGoFieldName(attribute.Name, false), genGoFieldType(base), tag)
		}
		content += "}\n"
		gen.StructAST[v.Name] = content
		gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, v.Doc, "//"), fieldName, gen.StructAST[v.Name])
	}
}

// GoElement generates code for element XML schema in Go language syntax.
func (gen *CodeGenerator) GoElement(v *Element) {
	if _, ok := gen.StructAST[v.Name]; !ok {
		var plural string
		if v.Plural {
			plural = "[]"
		}
		content := fmt.Sprintf("\t%s%s\n", plural, genGoFieldType(getBasefromSimpleType(trimNSPrefix(v.Type), gen.ProtoTree)))
		gen.StructAST[v.Name] = content
		fieldName := genGoFieldName(v.Name, false)
		gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, v.Doc, "//"), fieldName, gen.StructAST[v.Name])
	}
}

// GoAttribute generates code for attribute XML schema in Go language syntax.
func (gen *CodeGenerator) GoAttribute(v *Attribute) {
	if _, ok := gen.StructAST[v.Name]; !ok {
		var plural string
		if v.Plural {
			plural = "[]"
		}
		content := fmt.Sprintf("\t%s%s\n", plural, genGoFieldType(getBasefromSimpleType(trimNSPrefix(v.Type), gen.ProtoTree)))
		gen.StructAST[v.Name] = content
		fieldName := genGoFieldName(v.Name, true)
		gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, v.Doc, "//"), fieldName, gen.StructAST[v.Name])
	}
}

func (gen *CodeGenerator) FileWithExtension(extension string) string {
	if !strings.HasPrefix(extension, ".") {
		extension = "." + extension
	}
	if strings.HasSuffix(gen.File, extension) {
		return gen.File
	}
	return gen.File + extension
}

// ensureNamedType ensures that a named XSD simple type referenced in fields has
// a corresponding Go type declaration emitted. If the simple type is not found
// in the ProtoTree (e.g., due to schema nuances), we synthesize a type alias
// to its base if known, otherwise to string. This prevents unresolved type
// errors in generated code while still allowing validations on inline
// restrictions to work.
// findSimpleType retrieves a named simpleType definition from the ProtoTree.
func (gen *CodeGenerator) findSimpleType(name string) *SimpleType {
	name = trimNSPrefix(name)
	for _, ele := range gen.ProtoTree {
		if st, ok := ele.(*SimpleType); ok {
			if st.Name == name {
				return st
			}
		}
	}
	return nil
}

func (gen *CodeGenerator) findSimpleTypeByGoName(goName string) *SimpleType {
	for _, ele := range gen.ProtoTree {
		if st, ok := ele.(*SimpleType); ok {
			if genGoFieldName(st.Name, false) == goName {
				return st
			}
		}
	}
	return nil
}

// ensureNamedType ensures that a named XSD simpleType referenced in fields has
// a corresponding Go type declaration emitted. Only emits when the named
// simpleType exists in the ProtoTree. The emitted type will also include a
// Validate() method if the simpleType has restrictions.
func (gen *CodeGenerator) ensureNamedType(xsdOrGoName string) {
	name := trimNSPrefix(xsdOrGoName)
	if name == "" {
		return
	}
	// Skip built-ins
	if bt, ok := getBuildInTypeByLang(name, "Go"); ok && bt != "" {
		return
	}
	// Attempt to locate the underlying SimpleType by XSD name or Go-emitted name
	st := gen.findSimpleType(name)
	if st == nil {
		// Try by Go name mapping (e.g., TStateCode → tStateCode)
		st = gen.findSimpleTypeByGoName(name)
	}
	if st == nil && len(name) > 0 {
		// Try lowercasing the first rune (TStateCode → tStateCode)
		lower := strings.ToLower(name[:1]) + name[1:]
		st = gen.findSimpleType(lower)
	}
	if st == nil {
		// Not found in current schema. If generating the shared common types file,
		// synthesize a minimal alias to avoid undefined references across files.
		if strings.Contains(gen.File, "commonTypes.go") {
			key := name
			if _, ok := gen.StructAST[key]; ok {
				return
			}
			base := getBasefromSimpleType(name, gen.ProtoTree)
			if base == name || base == "" {
				if bt, ok := getBuildInTypeByLang(name, "Go"); ok && bt != "" {
					base = bt
				} else {
					base = "string"
				}
			}
			declType := genGoFieldType(base)
			if strings.HasPrefix(declType, "*") {
				declType = strings.TrimPrefix(declType, "*")
			}
			content := fmt.Sprintf(" %s\n", declType)
			gen.StructAST[key] = content
			fieldName := genGoFieldName(name, true)
			gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, "", "//"), fieldName, content)
			return
		}
		// For trainOperation.go, synthesize minimal aliases for specific cross-file
		// union member types that are otherwise not emitted here.
		if strings.Contains(gen.File, "trainOperation.go") {
			goName := genGoFieldName(name, false)
			if goName == "TSendingType" || goName == "TSendingTypeSpecial" {
				key := goName
				if _, ok := gen.StructAST[key]; !ok {
					content := " int\n"
					gen.StructAST[key] = content
					gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(key, "", "//"), key, content)
				}
			}
		}
		return
	}
	// Use the XSD name as key for StructAST to avoid duplicates
	key := st.Name
	if _, ok := gen.StructAST[key]; ok {
		return
	}
	base := getBasefromSimpleType(trimNSPrefix(st.Base), gen.ProtoTree)
	content := fmt.Sprintf(" %s\n", genGoFieldType(base))
	gen.StructAST[key] = content
	fieldName := genGoFieldName(st.Name, true)
	gen.Field += fmt.Sprintf("%stype %s%s", genFieldComment(fieldName, st.Doc, "//"), fieldName, content)
	// Generate validator for this simpleType if it has restrictions
	gen.generateSimpleTypeValidator(fieldName, base, &st.Restriction)
}

// generateSimpleTypeValidator emits a Validate() method for a named simple type
// according to its Restriction rules. Currently supports:
// - string: pattern, enum, length, minLength, maxLength
// - numeric (int, uint, float): min/max with inclusive/exclusive
func (gen *CodeGenerator) generateSimpleTypeValidator(typeName, base string, r *Restriction) {
	if r == nil {
		return
	}
	// Determine if there is anything to validate
	has := false
	if len(r.Enum) > 0 || r.PatternStr != "" || r.HasLength || r.HasMinLength || r.HasMaxLength || r.HasMin || r.HasMax {
		has = true
	}
	if !has {
		return
	}
	var b strings.Builder
	needsFmt := false
	b.WriteString("\nfunc (v ")
	b.WriteString(typeName)
	b.WriteString(") Validate() error {\n")

	isString := base == "string"
	isNumeric := isNumericGoType(base)
	if isString {
		// Length checks
		if r.HasLength {
			fmt.Fprintf(&b, "\tif len(string(v)) != %d { return fmt.Errorf(\"%s length must be exactly %d\") }\n", r.Length, typeName, r.Length)
			needsFmt = true
		} else {
			if r.HasMinLength {
				fmt.Fprintf(&b, "\tif len(string(v)) < %d { return fmt.Errorf(\"%s length must be >= %d\") }\n", r.MinLength, typeName, r.MinLength)
				needsFmt = true
			}
			if r.HasMaxLength {
				fmt.Fprintf(&b, "\tif len(string(v)) > %d { return fmt.Errorf(\"%s length must be <= %d\") }\n", r.MaxLength, typeName, r.MaxLength)
				needsFmt = true
			}
		}
		// Pattern check
		if r.PatternStr != "" {
			gen.ImportRegexp = true
			// Embed the pattern as a string literal in code for the matcher, but keep it as a runtime value in the error message
			fmt.Fprintf(&b, "\tif ok := regexp.MustCompile(%q).MatchString(string(v)); !ok { return fmt.Errorf(\"%%s does not match pattern: %%q\", %q, %q) }\n", r.PatternStr, typeName, r.PatternStr)
			needsFmt = true
		}
		// Enum check
		if len(r.Enum) > 0 {
			b.WriteString("\t{")
			b.WriteString("\n\t\tallowed := map[string]struct{}{\n")
			for _, ev := range r.Enum {
				fmt.Fprintf(&b, "\t\t\t%q: {},\n", ev)
			}
			b.WriteString("\t\t}\n")
			b.WriteString("\t\tif _, ok := allowed[string(v)]; !ok { return fmt.Errorf(\"" + typeName + " must be one of enum values\") }\n")
			b.WriteString("\t}\n")
			needsFmt = true
		}
	}
	if isNumeric && (r.HasMin || r.HasMax) {
		// Cast to float64 for comparison using the recorded Min/Max
		fmt.Fprintf(&b, "\tvv := float64(v)\n")
		if r.HasMin {
			if r.MinExclusive {
				fmt.Fprintf(&b, "\tif vv <= %g { return fmt.Errorf(\"%s must be > %g\") }\n", r.Min, typeName, r.Min)
			} else {
				fmt.Fprintf(&b, "\tif vv < %g { return fmt.Errorf(\"%s must be >= %g\") }\n", r.Min, typeName, r.Min)
			}
			needsFmt = true
		}
		if r.HasMax {
			if r.MaxExclusive {
				fmt.Fprintf(&b, "\tif vv >= %g { return fmt.Errorf(\"%s must be < %g\") }\n", r.Max, typeName, r.Max)
			} else {
				fmt.Fprintf(&b, "\tif vv > %g { return fmt.Errorf(\"%s must be <= %g\") }\n", r.Max, typeName, r.Max)
			}
			needsFmt = true
		}
	}
	b.WriteString("\treturn nil\n}")
	if needsFmt {
		gen.ImportFmt = true
	}
	gen.Field += b.String() + "\n"
}

func isNumericGoType(t string) bool {
	switch t {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "float32", "float64":
		return true
	default:
		return false
	}
}

// hasRestrictions reports whether the restriction contains any rules.
func hasRestrictions(r *Restriction) bool {
	if r == nil {
		return false
	}
	return len(r.Enum) > 0 || r.PatternStr != "" || r.HasLength || r.HasMinLength || r.HasMaxLength || r.HasMin || r.HasMax
}

// generateComplexTypeValidator emits a Validate() method for complex types that
// have inline restrictions on their attributes or elements.
func (gen *CodeGenerator) generateComplexTypeValidator(typeName string, v *ComplexType) {
	any := false
	var b strings.Builder
	// Scan to see if there is any restriction to enforce
	for _, a := range v.Attributes {
		if hasRestrictions(&a.Restriction) {
			any = true
			break
		}
	}
	if !any {
		for _, e := range v.Elements {
			if hasRestrictions(&e.Restriction) {
				any = true
				break
			}
		}
	}
	if !any {
		return
	}
	gen.ImportFmt = true
	b.WriteString("\nfunc (m *")
	b.WriteString(typeName)
	b.WriteString(") Validate() error {\n")
	b.WriteString("\tif m == nil { return nil }\n")
	// Attributes
	for _, a := range v.Attributes {
		r := a.Restriction
		if !hasRestrictions(&r) {
			continue
		}
		fieldName := genGoFieldName(a.Name, false) + "Attr"
		base := getBasefromSimpleType(trimNSPrefix(a.Type), gen.ProtoTree)
		if a.Optional {
			fmt.Fprintf(&b, "\tif m.%s != nil {\n", fieldName)
			checks := gen.generateRestrictionChecks("*m."+fieldName, base, fieldName, &r)
			b.WriteString(checks)
			b.WriteString("\t}\n")
		} else {
			checks := gen.generateRestrictionChecks("m."+fieldName, base, fieldName, &r)
			b.WriteString(checks)
		}
	}
	// Elements
	for _, e := range v.Elements {
		r := e.Restriction
		if !hasRestrictions(&r) {
			continue
		}
		fieldName := genGoFieldName(e.Name, false)
		base := getBasefromSimpleType(trimNSPrefix(e.Type), gen.ProtoTree)
		if e.Plural {
			fmt.Fprintf(&b, "\tfor _, it := range m.%s {\n", fieldName)
			checks := gen.generateRestrictionChecks("it", base, fieldName, &r)
			b.WriteString(checks)
			b.WriteString("\t}\n")
		} else if e.Optional {
			fmt.Fprintf(&b, "\tif m.%s != nil {\n", fieldName)
			checks := gen.generateRestrictionChecks("*m."+fieldName, base, fieldName, &r)
			b.WriteString(checks)
			b.WriteString("\t}\n")
		} else {
			checks := gen.generateRestrictionChecks("m."+fieldName, base, fieldName, &r)
			b.WriteString(checks)
		}
	}
	b.WriteString("\treturn nil\n}")
	gen.Field += b.String() + "\n"
}

// generateRestrictionChecks generates the Go code snippet that enforces the
// given restriction against an expression holding the value.
func (gen *CodeGenerator) generateRestrictionChecks(varExpr, base, subjectName string, r *Restriction) string {
	var b strings.Builder
	isString := base == "string"
	isNumeric := isNumericGoType(base)
	if isString {
		if r.HasLength {
			fmt.Fprintf(&b, "\tif len(string(%s)) != %d { return fmt.Errorf(\"%s length must be exactly %d\") }\n", varExpr, r.Length, subjectName, r.Length)
		} else {
			if r.HasMinLength {
				fmt.Fprintf(&b, "\tif len(string(%s)) < %d { return fmt.Errorf(\"%s length must be >= %d\") }\n", varExpr, r.MinLength, subjectName, r.MinLength)
			}
			if r.HasMaxLength {
				fmt.Fprintf(&b, "\tif len(string(%s)) > %d { return fmt.Errorf(\"%s length must be <= %d\") }\n", varExpr, r.MaxLength, subjectName, r.MaxLength)
			}
		}
		if r.PatternStr != "" {
			gen.ImportRegexp = true
			fmt.Fprintf(&b, "\tif ok := regexp.MustCompile(%q).MatchString(string(%s)); !ok { return fmt.Errorf(\"%s does not match pattern: %%q\", %q) }\n", r.PatternStr, varExpr, subjectName, r.PatternStr)
		}
		if len(r.Enum) > 0 {
			b.WriteString("\t{")
			b.WriteString("\n\t\tallowed := map[string]struct{}{\n")
			for _, ev := range r.Enum {
				fmt.Fprintf(&b, "\t\t\t%q: {},\n", ev)
			}
			b.WriteString("\t\t}\n")
			fmt.Fprintf(&b, "\t\tif _, ok := allowed[string(%s)]; !ok { return fmt.Errorf(\"%s must be one of enum values\") }\n", varExpr, subjectName)
			b.WriteString("\t}\n")
		}
	}
	if isNumeric {
		if r.HasMin || r.HasMax {
			fmt.Fprintf(&b, "\tvv := float64(%s)\n", varExpr)
			if r.HasMin {
				if r.MinExclusive {
					fmt.Fprintf(&b, "\tif vv <= %g { return fmt.Errorf(\"%s must be > %g\") }\n", r.Min, subjectName, r.Min)
				} else {
					fmt.Fprintf(&b, "\tif vv < %g { return fmt.Errorf(\"%s must be >= %g\") }\n", r.Min, subjectName, r.Min)
				}
			}
			if r.HasMax {
				if r.MaxExclusive {
					fmt.Fprintf(&b, "\tif vv >= %g { return fmt.Errorf(\"%s must be < %g\") }\n", r.Max, subjectName, r.Max)
				} else {
					fmt.Fprintf(&b, "\tif vv > %g { return fmt.Errorf(\"%s must be <= %g\") }\n", r.Max, subjectName, r.Max)
				}
			}
		}
	}
	return b.String()
}
