package integration

import (
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

// RequiredFields rejects omitted or null required values before decoding can replace them with zero values.
func RequiredFields(raw []byte, shape reflect.Type) bool {
	if shape.Kind() == reflect.Pointer {
		if string(raw) == "null" {
			return true
		}
		return RequiredFields(raw, shape.Elem())
	}
	if shape == reflect.TypeOf(time.Time{}) {
		return string(raw) != "null"
	}
	switch shape.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil || fields == nil {
			return false
		}
		for i := 0; i < shape.NumField(); i++ {
			field := shape.Field(i)
			if field.Anonymous && field.Tag.Get("json") == "" {
				if !RequiredFields(raw, field.Type) {
					return false
				}
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			name := tag[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			value, exists := fields[name]
			optional := false
			for _, part := range tag[1:] {
				if part == "omitempty" {
					optional = true
				}
			}
			if !exists {
				if !optional {
					return false
				}
				continue
			}
			if !RequiredFields(value, field.Type) {
				return false
			}
		}
	case reflect.Slice:
		if string(raw) == "null" {
			return true
		}
		var items []json.RawMessage
		if json.Unmarshal(raw, &items) != nil {
			return false
		}
		for _, item := range items {
			if !RequiredFields(item, shape.Elem()) {
				return false
			}
		}
	default:
		if string(raw) == "null" {
			return false
		}
	}
	return true
}
