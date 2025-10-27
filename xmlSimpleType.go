// Copyright 2020 - 2024 The xgen Authors. All rights reserved. Use of this
// source code is governed by a BSD-style license that can be found in the
// LICENSE file.
//
// Package xgen written in pure Go providing a set of functions that allow you
// to parse XSD (XML schema files). This library needs Go version 1.10 or
// later.

package xgen

import "encoding/xml"

// OnSimpleType handles parsing event on the simpleType start elements. The
// simpleType element defines a simple type and specifies the constraints and
// information about the values of attributes or text-only elements.
func (opt *Options) OnSimpleType(ele xml.StartElement, protoTree []interface{}) (err error) {
	// Start a new simple type scope when encountering a simpleType element.
	opt.SimpleType.Push(&SimpleType{})
	if opt.CurrentEle == "attributeGroup" {
		// keep parsing, attributeGroup can contain nested simpleTypes
	}

	for _, attr := range ele.Attr {
		if attr.Name.Local == "name" {
			opt.CurrentEle = opt.InElement
			opt.SimpleType.Peek().(*SimpleType).Name = attr.Value
		}
	}
	return
}

// EndSimpleType handles parsing event on the simpleType end elements.
func (opt *Options) EndSimpleType(ele xml.EndElement, protoTree []interface{}) (err error) {
	if opt.SimpleType.Len() == 0 {
		return
	}
	st := opt.SimpleType.Peek().(*SimpleType)
	// If this is an anonymous simpleType defined inline for an attribute, assign its base to the attribute.
	if opt.Attribute.Len() > 0 && st.Name == "" {
		opt.Attribute.Peek().(*Attribute).Type = st.Base
		opt.SimpleType.Pop()
		return
	}
	// If this is an anonymous simpleType defined inline for an element, assign its base to the element.
	if opt.Element.Len() > 0 && st.Name == "" {
		opt.Element.Peek().(*Element).Type = st.Base
		opt.SimpleType.Pop()
		return
	}
	// Persist named simpleTypes (top-level or nested) and anonymous non-inline unions/lists
	if !opt.InUnion {
		// temporary debug
		// fmt.Println("Add simpleType:", st.Name, "base:", st.Base)
		opt.ProtoTree = append(opt.ProtoTree, opt.SimpleType.Pop())
		opt.CurrentEle = ""
		return
	}
	return
}
