package field

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/tidwall/assert"
	"github.com/tidwall/btree"
)

func TestList(t *testing.T) {
	var fields List

	fields = fields.Set(Make("hello", "123"))
	assert.Assert(fields.Len() == 1)
	// println(fields.Weight())
	// assert.Assert(fields.Weight() == 16)

	fields = fields.Set(Make("jello", "456"))
	assert.Assert(fields.Len() == 2)
	// assert.Assert(fields.Weight() == 32)

	value := fields.Get("jello")
	assert.Assert(value.Value().Data() == "456")
	assert.Assert(value.Value().JSON() == "456")
	assert.Assert(value.Value().Num() == 456)

	value = fields.Get("nello")
	assert.Assert(value.Name() == "")
	assert.Assert(value.Value().IsZero())

	fields = fields.Set(Make("jello", "789"))
	assert.Assert(fields.Len() == 2)
	// assert.Assert(fields.Weight() == 32)

	fields = fields.Set(Make("nello", "0"))
	assert.Assert(fields.Len() == 2)
	// assert.Assert(fields.Weight() == 32)

	fields = fields.Set(Make("jello", "789"))
	assert.Assert(fields.Len() == 2)
	// assert.Assert(fields.Weight() == 32)

	fields = fields.Set(Make("jello", "0"))
	assert.Assert(fields.Len() == 1)
	// assert.Assert(fields.Weight() == 16)

	fields = fields.Set(Make("nello", "012"))
	fields = fields.Set(Make("hello", "456"))
	fields = fields.Set(Make("fello", "123"))
	fields = fields.Set(Make("jello", "789"))

	var names string
	var datas string
	var nums float64
	fields.Scan(func(f Field) bool {
		names += f.Name()
		datas += f.Value().Data()
		nums += f.Value().Num()
		return true
	})
	assert.Assert(names == "fellohellojellonello")
	assert.Assert(datas == "123456789012")
	assert.Assert(nums == 1368)

	names = ""
	datas = ""
	nums = 0
	fields.Scan(func(f Field) bool {
		names += f.Name()
		datas += f.Value().Data()
		nums += f.Value().Num()
		return false
	})
	assert.Assert(names == "fello")
	assert.Assert(datas == "123")
	assert.Assert(nums == 123)

}

func randStr(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := 0; i < n; i++ {
		b[i] = 'a' + b[i]%26
	}
	return string(b)
}

func randVal(n int) string {
	switch rand.Intn(10) {
	case 0:
		return "null"
	case 1:
		return "true"
	case 2:
		return "false"
	case 3:
		return `{"a":"` + randStr(n) + `"}`
	case 4:
		return `["` + randStr(n) + `"]`
	case 5:
		return `"` + randStr(n) + `"`
	case 6:
		return randStr(n)
	default:
		return fmt.Sprintf("%f", rand.Float64()*360)
	}
}

func TestRandom(t *testing.T) {
	seed := time.Now().UnixNano()
	// seed = 1663607868546669000
	rand.Seed(seed)
	start := time.Now()
	var total int
	for time.Since(start) < time.Second*2 {
		N := rand.Intn(500)
		var org []Field
		var tr btree.Map[string, Field]
		var fields List
		for i := 0; i < N; i++ {
			name := randStr(rand.Intn(10))
			value := randVal(rand.Intn(10))
			field := Make(name, value)
			org = append(org, field)
			fields = fields.Set(field)
			v := fields.Get(name)
			// println(name, v.Value().Data(), field.Value().Data())
			if !v.Value().Equals(field.Value()) {
				t.Fatalf("seed: %d, expected true", seed)
			}
			tr.Set(name, field)
			if fields.Len() != tr.Len() {
				t.Fatalf("seed: %d, expected %d, got %d",
					seed, tr.Len(), fields.Len())
			}
		}
		comp := func() {
			var all []Field
			fields.Scan(func(f Field) bool {
				all = append(all, f)
				return true
			})
			if len(all) != fields.Len() {
				t.Fatalf("seed: %d, expected %d, got %d",
					seed, fields.Len(), len(all))
			}
			if fields.Len() != tr.Len() {
				t.Fatalf("seed: %d, expected %d, got %d",
					seed, tr.Len(), fields.Len())
			}
			var i int
			tr.Scan(func(name string, f Field) bool {
				if name != f.Name() || all[i].Name() != f.Name() {
					t.Fatalf("seed: %d, out of order", seed)
				}
				i++
				return true
			})
		}
		comp()
		rand.Shuffle(len(org), func(i, j int) {
			org[i], org[j] = org[j], org[i]
		})
		for _, f := range org {
			comp()
			tr.Delete(f.Name())
			fields = fields.Set(Make(f.Name(), "0"))
			if fields.Len() != tr.Len() {
				t.Fatalf("seed: %d, expected %d, got %d",
					seed, tr.Len(), fields.Len())
			}
			comp()
		}
		total++
	}

}

func TestJSONGet(t *testing.T) {

	var list List
	list = list.Set(Make("hello", "world"))
	list = list.Set(Make("hello", `"world"`))
	list = list.Set(Make("jello", "planet"))
	list = list.Set(Make("telly", `{"a":[1,2,3],"b":null,"c":true,"d":false}`))
	list = list.Set(Make("belly", `{"a":{"b":{"c":"fancy"}}}`))
	json := list.String()
	exp := `{"belly":{"a":{"b":{"c":"fancy"}}},"hello":"world","jello":` +
		`"planet","telly":{"a":[1,2,3],"b":null,"c":true,"d":false}}`
	if json != exp {
		t.Fatalf("expected '%s', got '%s'", exp, json)
	}
	data := list.Get("hello").Value().Data()
	if data != "world" {
		t.Fatalf("expected '%s', got '%s'", "world", data)
	}
	data = list.Get("telly").Value().Data()
	if data != `{"a":[1,2,3],"b":null,"c":true,"d":false}` {
		t.Fatalf("expected '%s', got '%s'",
			`{"a":[1,2,3],"b":null,"c":true,"d":false}`, data)
	}
	data = list.Get("belly").Value().Data()
	if data != `{"a":{"b":{"c":"fancy"}}}` {
		t.Fatalf("expected '%s', got '%s'",
			`{"a":{"b":{"c":"fancy"}}}`, data)
	}
	data = list.Get("belly.a").Value().Data()
	if data != `{"b":{"c":"fancy"}}` {
		t.Fatalf("expected '%s', got '%s'",
			`{"b":{"c":"fancy"}}`, data)
	}
	data = list.Get("belly.a.b").Value().Data()
	if data != `{"c":"fancy"}` {
		t.Fatalf("expected '%s', got '%s'",
			`{"c":"fancy"}`, data)
	}
	data = list.Get("belly.a.b.c").Value().Data()
	if data != `fancy` {
		t.Fatalf("expected '%s', got '%s'",
			`fancy`, data)
	}
	// Tile38 defaults non-existent fields to zero.
	data = list.Get("belly.a.b.c.d").Value().Data()
	if data != `0` {
		t.Fatalf("expected '%s', got '%s'",
			`0`, data)
	}
}
