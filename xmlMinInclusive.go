// Copyright 2020 - 2024 The xgen Authors. All rights reserved. Use of this
// source code is governed by a BSD-style license that can be found in the
// LICENSE file.
//
// Package xgen written in pure Go providing a set of functions that allow you
// to parse XSD (XML schema files). This library needs Go version 1.10 or
// later.

package xgen

import (
	"encoding/xml"
	"strconv"
)

// OnMinInclusive handles parsing event on the minInclusive start element.
func (opt *Options) OnMinInclusive(ele xml.StartElement, protoTree []interface{}) (err error) {
	for _, attr := range ele.Attr {
		if attr.Name.Local == "value" {
			if st, ok := opt.SimpleType.Peek().(*SimpleType); ok && st != nil {
				if v, e := strconv.ParseFloat(attr.Value, 64); e == nil {
					st.Restriction.Min = v
					st.Restriction.HasMin = true
					st.Restriction.MinExclusive = false
				}
			}
		}
	}
	return
}

// EndMinInclusive handles parsing event on the minInclusive end elements.
// MinInclusive specifies the lower bounds for numeric values (the value must
// be greater than or equal to this value).
func (opt *Options) EndMinInclusive(ele xml.EndElement, protoTree []interface{}) (err error) {
	return
}
