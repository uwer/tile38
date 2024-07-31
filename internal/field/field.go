package field

import (
	"math"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
)

var ZeroValue = Value{kind: Number, data: "0", num: 0}
var ZeroField = Field{name: "", value: ZeroValue}

type Kind byte

const (
	Null   = Kind(gjson.Null)
	False  = Kind(gjson.False)
	Number = Kind(gjson.Number)
	String = Kind(gjson.String)
	True   = Kind(gjson.True)
	JSON   = Kind(gjson.JSON)
)

type Value struct {
	kind Kind
	data string
	num  float64
}

func (v Value) IsZero() bool {
	return (v.kind == Number && v.data == "0" && v.num == 0) || v == (Value{})
}

func (v Value) Equals(b Value) bool {
	return !v.Less(b) && !b.Less(v)
}

func (v Value) Kind() Kind {
	return v.kind
}

func (v Value) Data() string {
	return v.data
}

func (v Value) Num() float64 {
	return v.num
}

func (v Value) JSON() string {
	switch v.Kind() {
	case Number:
		switch v.Data() {
		case "NaN":
			return `"NaN"`
		case "+Inf":
			return `"+Inf"`
		case "-Inf":
			return `"-Inf"`
		default:
			return v.Data()
		}
	case String:
		return string(gjson.AppendJSONString(nil, v.Data()))
	case True:
		return "true"
	case False:
		return "false"
	case Null:
		if v != (Value{}) {
			return "null"
		}
	case JSON:
		return v.Data()
	}
	return "0"
}

func stringLessInsensitive(a, b string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] >= 'A' && a[i] <= 'Z' {
			if b[i] >= 'A' && b[i] <= 'Z' {
				// both are uppercase, do nothing
				if a[i] < b[i] {
					return true
				} else if a[i] > b[i] {
					return false
				}
			} else {
				// a is uppercase, convert a to lowercase
				if a[i]+32 < b[i] {
					return true
				} else if a[i]+32 > b[i] {
					return false
				}
			}
		} else if b[i] >= 'A' && b[i] <= 'Z' {
			// b is uppercase, convert b to lowercase
			if a[i] < b[i]+32 {
				return true
			} else if a[i] > b[i]+32 {
				return false
			}
		} else {
			// neither are uppercase
			if a[i] < b[i] {
				return true
			} else if a[i] > b[i] {
				return false
			}
		}
	}
	return len(a) < len(b)
}

// Less return true if a value is less than another value.
// The caseSensitive parameter is used when the value are Strings.
// The order when comparing two different kinds is:
//
//	Null < False < Number < String < True < JSON
//
// Pulled from github.com/tidwall/gjson
func (v Value) LessCase(b Value, caseSensitive bool) bool {
	if v.kind < b.kind {
		return true
	}
	if v.kind > b.kind {
		return false
	}
	if v.kind == Number {
		return v.num < b.num
	}
	if v.kind == String {
		if caseSensitive {
			return v.data < b.data
		}
		return stringLessInsensitive(v.data, b.data)
	}
	return v.data < b.data
}

// Less return true if a value is less than another value.
//
//	Null < False < Number < String < True < JSON
//
// Pulled from github.com/tidwall/gjson
func (v Value) Less(b Value) bool {
	return v.LessCase(b, false)
}

type Field struct {
	name  string
	value Value
}

func (f Field) Name() string {
	return f.name
}

func (f Field) Value() Value {
	return f.value
}

func (f Field) Weight() int {
	return len(f.name) + 8 + len(f.value.data)
}

var nan = math.NaN()
var pinf = math.Inf(+1)
var ninf = math.Inf(-1)

func ValueOf(data string) Value {
	data = strings.TrimSpace(data)
	num, err := strconv.ParseFloat(data, 64)
	if err == nil {
		if math.IsInf(num, 0) {
			if math.IsInf(num, +1) {
				return Value{kind: Number, data: "+Inf", num: pinf}
			} else {
				return Value{kind: Number, data: "-Inf", num: ninf}
			}
		} else if math.IsNaN(num) {
			return Value{kind: Number, data: "NaN", num: nan}
		}
		// Make sure that this is a JSON compatible number.
		// For example, "000123" and "000_123" both parse as floats but aren't
		// really Numbers that can be represents in JSON.
		if gjson.Valid(data) {
			return Value{kind: Number, data: data, num: num}
		}
	} else if gjson.Valid(data) {
		data = strings.TrimSpace(data)
		r := gjson.Parse(data)
		switch r.Type {
		case gjson.Null:
			return Value{kind: Null, data: "null"}
		case gjson.JSON:
			return Value{kind: JSON, data: string(pretty.Ugly([]byte(data)))}
		case gjson.True:
			return Value{kind: True, data: "true"}
		case gjson.False:
			return Value{kind: False, data: "false"}
		case gjson.Number:
			// Ignore. Numbers will always be picked up by the ParseFloat above.
		case gjson.String:
			// Ignore. Strings fallthrough by default
		}
		// Extract String from JSON
		data = r.String()
	}
	// Check if string is NaN, Inf(inity), +Inf(inity), -Inf(inity)
	if len(data) >= 3 && len(data) <= 9 {
		switch data[0] {
		case '-', '+', 'I', 'i', 'N', 'n':
			switch strings.ToLower(data) {
			case "nan":
				return Value{kind: Number, data: "NaN", num: nan}
			case "inf", "+inf", "infinity", "+infinity":
				return Value{kind: Number, data: "+Inf", num: pinf}
			case "-inf", "-infinity":
				return Value{kind: Number, data: "-Inf", num: ninf}
			}
		}
	}

	return Value{kind: String, data: data}
}

func Make(name, data string) Field {
	return Field{
		strings.TrimSpace(name),
		ValueOf(data),
	}
}
