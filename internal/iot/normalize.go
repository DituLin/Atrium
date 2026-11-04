package iot

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

type Normalized struct {
	Value        any
	Unit         string
	Availability Availability
	Reason       Reason
}

// Normalize validates provider data without including raw data in failure reasons.
// Numeric readings require explicit units; output temperature is always Celsius.
func Normalize(kind Kind, raw any, unit string, values map[string]string) Normalized {
	fail := func(a Availability, r Reason) Normalized { return Normalized{Availability: a, Reason: r} }
	if raw == nil {
		return fail(Unknown, ReasonMissing)
	}
	if s, ok := raw.(string); ok {
		switch strings.TrimSpace(s) {
		case "unknown":
			return fail(Unknown, ReasonUnknown)
		case "unavailable":
			return fail(Unavailable, ReasonUnavailable)
		}
	}
	out := Normalized{Availability: Available}
	switch kind {
	case Power:
		if unit != "" {
			return fail(Unknown, ReasonInvalid)
		}
		switch v := raw.(type) {
		case bool:
			out.Value = v
		case string:
			switch v {
			case "on", "true":
				out.Value = true
			case "off", "false":
				out.Value = false
			default:
				return fail(Unknown, ReasonInvalid)
			}
		default:
			return fail(Unknown, ReasonInvalid)
		}
	case OperatingState:
		s, ok := raw.(string)
		if !ok || unit != "" {
			return fail(Unknown, ReasonInvalid)
		}
		mapped, ok := values[s]
		if !ok || !ValidOperatingState(mapped) {
			return fail(Unknown, ReasonInvalid)
		}
		out.Value = mapped
	case Humidity, Battery, FilterLife, Temperature:
		n, ok := number(raw)
		if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
			return fail(Unknown, ReasonInvalid)
		}
		if kind == Temperature {
			switch unit {
			case "°C", "C":
			case "°F", "F":
				n = (n - 32) * 5 / 9
			default:
				return fail(Unknown, ReasonInvalid)
			}
			if n < -273.15 || math.IsInf(n, 0) || math.IsNaN(n) {
				return fail(Unknown, ReasonInvalid)
			}
			out.Unit = "°C"
		} else {
			if unit != "%" || n < 0 || n > 100 {
				return fail(Unknown, ReasonInvalid)
			}
			out.Unit = "%"
		}
		out.Value = n
	default:
		return fail(Unknown, ReasonInvalid)
	}
	return out
}
func number(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, e := v.Float64()
		return n, e == nil
	case string:
		n, e := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return n, e == nil
	}
	return 0, false
}
