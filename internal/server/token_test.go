package server

import (
	"strings"
	"testing"

	"github.com/tidwall/tile38/internal/field"
)

func TestLowerCompare(t *testing.T) {
	if !lc("hello", "hello") {
		t.Fatal("failed")
	}
	if !lc("Hello", "hello") {
		t.Fatal("failed")
	}
	if !lc("HeLLo World", "hello world") {
		t.Fatal("failed")
	}
	if !lc("", "") {
		t.Fatal("failed")
	}
	if lc("hello", "") {
		t.Fatal("failed")
	}
	if lc("", "hello") {
		t.Fatal("failed")
	}
	if lc("HeLLo World", "Hello world") {
		t.Fatal("failed")
	}
}

func TestParseWhereins(t *testing.T) {
	s := &Server{}

	type tcase struct {
		inputWhereins []whereinT
		expWhereins   []whereinT
	}

	fn := func(tc tcase) func(t *testing.T) {
		return func(t *testing.T) {

			_, tout, err := s.parseSearchScanBaseTokens(
				"scan",
				searchScanBaseTokens{
					whereins: tc.inputWhereins,
				},
				[]string{"key"},
			)
			got := tout.whereins
			exp := tc.expWhereins

			if err != nil {
				t.Fatalf("unexpected error while parsing search scan base tokens")
			}

			if len(got) != len(exp) {
				t.Fatalf("expected equal length whereins")
			}

			for i := range got {
				if got[i].name != exp[i].name {
					t.Fatalf("expected equal field names")
				}

				for j := range exp[i].valArr {
					if !got[i].match(exp[i].valArr[j]) {
						t.Fatalf("expected matching value arrays")
					}
				}
			}
		}
	}

	tests := map[string]tcase{
		"upper case": {
			inputWhereins: []whereinT{
				{
					name: "TEST",
					valArr: []field.Value{
						field.ValueOf("1"),
						field.ValueOf("1"),
					},
				},
			},
			expWhereins: []whereinT{
				{
					name: "TEST",
					valArr: []field.Value{
						field.ValueOf("1"),
						field.ValueOf("1"),
					},
				},
			},
		},
		"lower case": {
			inputWhereins: []whereinT{
				{
					name: "test",
					valArr: []field.Value{
						field.ValueOf("1"),
						field.ValueOf("1"),
					},
				},
			},
			expWhereins: []whereinT{
				{
					name: "test",
					valArr: []field.Value{
						field.ValueOf("1"),
						field.ValueOf("1"),
					},
				},
			},
		},
		"mixed case": {
			inputWhereins: []whereinT{
				{
					name: "teSt",
					valArr: []field.Value{
						field.ValueOf("1"),
						field.ValueOf("1"),
					},
				},
			},
			expWhereins: []whereinT{
				{
					name: "teSt",
					valArr: []field.Value{
						field.ValueOf("1"),
						field.ValueOf("1"),
					},
				},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, fn(tc))
	}

}

// func testParseFloat(t testing.TB, s string, f float64, invalid bool) {
// 	n, err := parseFloat(s)
// 	if err != nil {
// 		if invalid {
// 			return
// 		}
// 		t.Fatal(err)
// 	}
// 	if invalid {
// 		t.Fatalf("expecting an error for %s", s)
// 	}
// 	if n != f {
// 		t.Fatalf("for '%s', expect %f, got %f", s, f, n)
// 	}
// }

// func TestParseFloat(t *testing.T) {
// 	testParseFloat(t, "100", 100, false)
// 	testParseFloat(t, "0", 0, false)
// 	testParseFloat(t, "-1", -1, false)
// 	testParseFloat(t, "-0", -0, false)

// 	testParseFloat(t, "-100", -100, false)
// 	testParseFloat(t, "-0", -0, false)
// 	testParseFloat(t, "+1", 1, false)
// 	testParseFloat(t, "+0", 0, false)

// 	testParseFloat(t, "33.102938", 33.102938, false)
// 	testParseFloat(t, "-115.123123", -115.123123, false)

// 	testParseFloat(t, ".1", 0.1, false)
// 	testParseFloat(t, "0.1", 0.1, false)

// 	testParseFloat(t, "00.1", 0.1, false)
// 	testParseFloat(t, "01.1", 1.1, false)
// 	testParseFloat(t, "01", 1, false)
// 	testParseFloat(t, "-00.1", -0.1, false)
// 	testParseFloat(t, "+00.1", 0.1, false)
// 	testParseFloat(t, "", 0.1, true)
// 	testParseFloat(t, " 0", 0.1, true)
// 	testParseFloat(t, "0 ", 0.1, true)

// }

func BenchmarkLowerCompare(t *testing.B) {
	for i := 0; i < t.N; i++ {
		if !lc("HeLLo World", "hello world") {
			t.Fatal("failed")
		}
	}
}

func BenchmarkStringsLowerCompare(t *testing.B) {
	for i := 0; i < t.N; i++ {
		if strings.ToLower("HeLLo World") != "hello world" {
			t.Fatal("failed")
		}

	}
}

// func BenchmarkParseFloat(t *testing.B) {
// 	s := []string{"33.10293", "-115.1203102"}
// 	for i := 0; i < t.N; i++ {
// 		_, err := parseFloat(s[i%2])
// 		if err != nil {
// 			t.Fatal("failed")
// 		}
// 	}
// }

// func BenchmarkStrconvParseFloat(t *testing.B) {
// 	s := []string{"33.10293", "-115.1203102"}
// 	for i := 0; i < t.N; i++ {
// 		_, err := strconv.ParseFloat(s[i%2], 64)
// 		if err != nil {
// 			t.Fatal("failed")
// 		}
// 	}
// }
