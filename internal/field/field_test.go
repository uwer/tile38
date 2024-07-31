package field

import (
	"testing"

	"github.com/tidwall/assert"
)

func mLT(a, b Value) bool  { return a.Less(b) }
func mLTE(a, b Value) bool { return !mLT(b, a) }
func mGT(a, b Value) bool  { return mLT(b, a) }
func mGTE(a, b Value) bool { return !mLT(a, b) }
func mEQ(a, b Value) bool  { return !mLT(a, b) && !mLT(b, a) }

func TestOrder(t *testing.T) {
	assert.Assert(mLT(ValueOf("hello"), ValueOf("jello")))
	assert.Assert(mLT(ValueOf("hello"), ValueOf("JELLO")))
	assert.Assert(mLT(ValueOf("HELLO"), ValueOf("JELLO")))
	assert.Assert(mLT(ValueOf("HELLO"), ValueOf("jello")))
	assert.Assert(!mLT(ValueOf("hello"), ValueOf("hello")))
	assert.Assert(!mLT(ValueOf("jello"), ValueOf("hello")))
	assert.Assert(!mLT(ValueOf("Jello"), ValueOf("Hello")))
	assert.Assert(!mLT(ValueOf("Jello"), ValueOf("hello")))
	assert.Assert(!mLT(ValueOf("jello"), ValueOf("Hello")))
	assert.Assert(mGT(ValueOf("jello"), ValueOf("hello")))
	assert.Assert(!mGT(ValueOf("jello"), ValueOf("jello")))
	assert.Assert(!mGT(ValueOf("hello"), ValueOf("jello")))
	assert.Assert(mLTE(ValueOf("hello"), ValueOf("jello")))
	assert.Assert(mLTE(ValueOf("hello"), ValueOf("hello")))
	assert.Assert(mLTE(ValueOf("hello"), ValueOf("HELLO")))
	assert.Assert(!mLTE(ValueOf("jello"), ValueOf("hello")))
	assert.Assert(mGTE(ValueOf("jello"), ValueOf("jello")))
	assert.Assert(mGTE(ValueOf("jello"), ValueOf("hello")))
	assert.Assert(mGTE(ValueOf("jello"), ValueOf("JELLO")))
	assert.Assert(!mGTE(ValueOf("hello"), ValueOf("jello")))
	assert.Assert(mEQ(ValueOf("jello"), ValueOf("jello")))
	assert.Assert(mEQ(ValueOf("jello"), ValueOf("JELLO")))
	assert.Assert(!mEQ(ValueOf("jello"), ValueOf("hello")))
}

func TestLess(t *testing.T) {
	assert.Assert(mLT(ValueOf("null"), ValueOf("false")))
	assert.Assert(mLT(ValueOf("null"), ValueOf("123")))
	assert.Assert(mLT(ValueOf("null"), ValueOf("hello")))
	assert.Assert(mLT(ValueOf("null"), ValueOf("true")))
	assert.Assert(mLT(ValueOf("null"), ValueOf("[]")))
	assert.Assert(mLT(ValueOf("false"), ValueOf("123")))
	assert.Assert(mLT(ValueOf("false"), ValueOf("hello")))
	assert.Assert(mLT(ValueOf("false"), ValueOf("true")))
	assert.Assert(mLT(ValueOf("false"), ValueOf("[]")))
	assert.Assert(mLT(ValueOf("123"), ValueOf("hello")))
	assert.Assert(mLT(ValueOf("123"), ValueOf("true")))
	assert.Assert(mLT(ValueOf("123"), ValueOf("[]")))
	assert.Assert(mLT(ValueOf("hello"), ValueOf("true")))
	assert.Assert(mLT(ValueOf("hello"), ValueOf("[]")))
	assert.Assert(mLT(ValueOf("true"), ValueOf("[]")))
	assert.Assert(!mLT(ValueOf("false"), ValueOf("null")))
	assert.Assert(!mLT(ValueOf("123"), ValueOf("null")))
	assert.Assert(!mLT(ValueOf("hello"), ValueOf("null")))
	assert.Assert(!mLT(ValueOf("true"), ValueOf("null")))
	assert.Assert(!mLT(ValueOf("[]"), ValueOf("null")))
	assert.Assert(!mLT(ValueOf("123"), ValueOf("false")))
	assert.Assert(!mLT(ValueOf("hello"), ValueOf("false")))
	assert.Assert(!mLT(ValueOf("true"), ValueOf("false")))
	assert.Assert(!mLT(ValueOf("[]"), ValueOf("false")))
	assert.Assert(!mLT(ValueOf("hello"), ValueOf("123")))
	assert.Assert(!mLT(ValueOf("true"), ValueOf("123")))
	assert.Assert(!mLT(ValueOf("[]"), ValueOf("123")))
	assert.Assert(!mLT(ValueOf("true"), ValueOf("hello")))
	assert.Assert(!mLT(ValueOf("[]"), ValueOf("hello")))
	assert.Assert(!mLT(ValueOf("[]"), ValueOf("true")))
	assert.Assert(mLT(ValueOf("123"), ValueOf("456")))
	assert.Assert(mLT(ValueOf("[1]"), ValueOf("[2]")))
}

func TestLessCase(t *testing.T) {
	assert.Assert(ValueOf("A").LessCase(ValueOf("B"), true))
	assert.Assert(!ValueOf("A").LessCase(ValueOf("A"), true))
	assert.Assert(!ValueOf("B").LessCase(ValueOf("A"), true))
}

func TestVarious(t *testing.T) {
	assert.Assert(!ValueOf("A").IsZero())
	assert.Assert(ValueOf("0").IsZero())
	assert.Assert(Value{}.IsZero())
	assert.Assert(ZeroValue.IsZero())
	assert.Assert(ZeroValue.Equals(ZeroValue))
	assert.Assert(ZeroValue.Kind() == Number)
	assert.Assert(ValueOf("0").Kind() == Number)
	assert.Assert(ValueOf("hello").Kind() == String)
	assert.Assert(ValueOf(`"hello"`).Kind() == String)
	assert.Assert(ValueOf(`"123"`).Kind() == String)
	assert.Assert(ValueOf(`"123"`).Data() == `123`)
	assert.Assert(ValueOf(`"123"`).Num() == 0)
}

func TestJSON(t *testing.T) {
	assert.Assert(ValueOf(`A`).JSON() == `"A"`)
	assert.Assert(ValueOf(`"A"`).JSON() == `"A"`)
	assert.Assert(ValueOf(`123`).JSON() == `123`)
	assert.Assert(ValueOf(`{}`).JSON() == `{}`)
	assert.Assert(ValueOf(`{  }`).JSON() == `{}`)
	assert.Assert(ValueOf(` -Inf `).JSON() == `"-Inf"`)
	assert.Assert(ValueOf(` "-Inf" `).JSON() == `"-Inf"`)
	assert.Assert(ValueOf(`+Inf`).JSON() == `"+Inf"`)
	assert.Assert(ValueOf(`"+Inf"`).JSON() == `"+Inf"`)
	assert.Assert(ValueOf(`Inf`).JSON() == `"+Inf"`)
	assert.Assert(ValueOf(`"Inf"`).JSON() == `"+Inf"`)
	assert.Assert(ValueOf(`NaN`).JSON() == `"NaN"`)
	assert.Assert(ValueOf(`"NaN"`).JSON() == `"NaN"`)
	assert.Assert(ValueOf(`nan`).JSON() == `"NaN"`)
	assert.Assert(ValueOf(`infinity`).JSON() == `"+Inf"`)
	assert.Assert(ValueOf(` true `).JSON() == `true`)
	assert.Assert(ValueOf(` false `).JSON() == `false`)
	assert.Assert(ValueOf(` null `).JSON() == `null`)
	assert.Assert(Value{}.JSON() == `0`)
	assert.Assert(Value{}.JSON() == `0`)
}

func TestField(t *testing.T) {
	assert.Assert(Make("hello", "123").Name() == "hello")
	assert.Assert(Make("HELLO", "123").Name() == "HELLO")
	assert.Assert(Make("HELLO", "123").Value().Num() == 123)
	assert.Assert(Make("HELLO", "123").Value().JSON() == "123")
	assert.Assert(Make("HELLO", "123").Value().Num() == 123)
}

func TestWeight(t *testing.T) {
	assert.Assert(Make("hello", "123").Weight() == 16)
}

func TestNumber(t *testing.T) {
	assert.Assert(ValueOf("12").Num() == 12)
	assert.Assert(ValueOf("012").Num() == 0)
}
