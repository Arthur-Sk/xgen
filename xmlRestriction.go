// Copyright 2020 - 2024 The xgen Authors. All rights reserved. Use of this
// source code is governed by a BSD-style license that can be found in the
// LICENSE file.
//
// Package xgen written in pure Go providing a set of functions that allow you
// to parse XSD (XML schema files). This library needs Go version 1.10 or
// later.

package xgen

import "encoding/xml"

// OnRestriction handles parsing event on the restriction start elements. The
// restriction element defines restrictions on a simpleType, simpleContent, or
// complexContent definition.
func (opt *Options) OnRestriction(ele xml.StartElement, protoTree []interface{}) (err error) {
	for _, attr := range ele.Attr {
		if attr.Name.Local == "base" {
			var valueType string
			valueType, err = opt.GetValueType(attr.Value, protoTree)
			if err != nil {
				return
			}
			if opt.SimpleType.Peek() != nil {
				// Record the base on the current simpleType; defer applying to element/attribute until EndRestriction
				opt.SimpleType.Peek().(*SimpleType).Base = valueType
			}
		}
	}
	return
}

// EndRestriction handles parsing event on the restriction end elements.
func (opt *Options) EndRestriction(ele xml.EndElement, protoTree []interface{}) (err error) {
	if opt.SimpleType.Peek() == nil {
		return
	}
	// Only apply and pop for inline restrictions within attribute/element
	if opt.Attribute.Len() > 0 {
		st := opt.SimpleType.Pop().(*SimpleType)
		attr := opt.Attribute.Peek().(*Attribute)
		attr.Type, err = opt.GetValueType(st.Base, opt.ProtoTree)
		if err != nil {
			return
		}
		attr.Restriction = st.Restriction
		opt.CurrentEle = ""
		return
	}
	if opt.Element.Len() > 0 {
		st := opt.SimpleType.Pop().(*SimpleType)
		ele := opt.Element.Peek().(*Element)
		ele.Type, err = opt.GetValueType(st.Base, opt.ProtoTree)
		if err != nil {
			return
		}
		ele.Restriction = st.Restriction
		opt.CurrentEle = ""
		if !opt.ComplexType.Empty() && len(opt.ComplexType.Peek().(*ComplexType).Elements) > 0 {
			opt.ComplexType.Peek().(*ComplexType).Elements[len(opt.ComplexType.Peek().(*ComplexType).Elements)-1] = *ele
		}
	}
	// For named simpleType restrictions, keep the simpleType on stack; EndSimpleType will handle popping and persisting
	return
}
