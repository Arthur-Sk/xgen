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

// OnMaxLength handles parsing event on the maxLength start element.
func (opt *Options) OnMaxLength(ele xml.StartElement, protoTree []interface{}) (err error) {
	for _, attr := range ele.Attr {
		if attr.Name.Local == "value" {
			if st, ok := opt.SimpleType.Peek().(*SimpleType); ok && st != nil {
				if v, e := strconv.Atoi(attr.Value); e == nil {
					st.Restriction.MaxLength = v
					st.Restriction.HasMaxLength = true
				}
			}
		}
	}
	return
}

// EndMaxLength handles parsing event on the maxLength end elements. MaxLength
// specifies the maximum number of characters or list items allowed. Must be
// equal to or greater than zero.
func (opt *Options) EndMaxLength(ele xml.EndElement, protoTree []interface{}) (err error) {
	return
}
