package field

import (
	"encoding/binary"
	"strconv"
	"strings"
	"unsafe"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/tile38/internal/sstring"
)

// binary format
//   (size,entry,[entry...])
//   size: uvarint            -- size of the full byte slice, excluding itself.
//   entry: (name,value)      -- one field entry
//   name: shared string num  -- field name, string data, uses the shared library
//   size: uvarint            -- number of bytes in data
//   value: (kind,vdata)      -- field value
//   kind: byte               -- value kind
//   vdata: (size,data)       -- value data, string data

// useSharedNames will results in smaller memory usage by sharing the names
// of fields using the sstring package. Otherwise the names are embedded with
// the list.
const useSharedNames = true

// List of fields, ordered by Name.
type List struct {
	p *byte
}

type bytes struct {
	p *byte
	l int
	c int
}

func ptob(p *byte) []byte {
	if p == nil {
		return nil
	}
	// Get the size of the bytes (excluding the header)
	x, n := uvarint(*(*[]byte)(unsafe.Pointer(&bytes{p, 10, 10})))
	// Return the byte slice (excluding the header)
	return (*(*[]byte)(unsafe.Pointer(&bytes{p, n + x, n + x})))[n:]
}

func btoa(b []byte) string {
	return *(*string)(unsafe.Pointer(&b))
}

// uvarint is a slightly modified version of binary.Uvarint, and it's a little
// faster. But it lacks overflow checks which are not needed for our use.
func uvarint(buf []byte) (int, int) {
	var x uint64
	for i := 0; i < len(buf); i++ {
		b := buf[i]
		if b < 0x80 {
			return int(x | uint64(b)<<(i*7)), i + 1
		}
		x |= uint64(b&0x7f) << (i * 7)
	}
	return 0, 0
}

func datakind(kind Kind) bool {
	switch kind {
	case Number, String, JSON:
		return true
	}
	return false
}

func bfield(name string, kind Kind, data string) Field {
	var num float64
	switch kind {
	case Number:
		num, _ = strconv.ParseFloat(data, 64)
	case Null:
		data = "null"
	case False:
		data = "false"
	case True:
		data = "true"
	}
	return Field{
		name: name,
		value: Value{
			kind: Kind(kind),
			data: data,
			num:  num,
		},
	}
}

// Set a field in the list.
// If the input field value is zero `f.Value().IsZero()` then the field is
// deleted or removed from the list since lists cannot have Zero values.
// Returns a newly allocated list the updated field.
// The original (receiver) list is not modified.
func (fields List) Set(field Field) List {
	b := ptob(fields.p)
	var i int
	for {
		s := i
		// read the name
		var name string
		x, n := uvarint(b[i:])
		if n == 0 {
			break
		}
		if useSharedNames {
			name = sstring.Load(x)
			i += n
		} else {
			name = btoa(b[i+n : i+n+x])
			i += n + x
		}
		kind := Kind(b[i])
		i++
		var data string
		if datakind(kind) {
			x, n = uvarint(b[i:])
			data = btoa(b[i+n : i+n+x])
			i += n + x
		}
		if field.name < name {
			// insert before
			i = s
			break
		}
		if name == field.name {
			if field.Value().IsZero() {
				// delete
				return List{delfield(b, s, i)}
			}
			prev := bfield(name, kind, data)
			if prev.Value().Equals(field.Value()) {
				// no change
				return fields
			}
			// replace
			return List{putfield(b, field, s, i)}
		}
	}
	if field.Value().IsZero() {
		return fields
	}
	// insert after
	return List{putfield(b, field, i, i)}
}

func delfield(b []byte, s, e int) *byte {
	totallen := s + (len(b) - e)
	if totallen == 0 {
		return nil
	}
	var psz [10]byte
	pn := binary.PutUvarint(psz[:], uint64(totallen))
	plen := pn + totallen
	p := make([]byte, plen)
	// copy each component
	i := 0

	// -- header size
	copy(p[i:], psz[:pn])
	i += pn

	// -- head entries
	copy(p[i:], b[:s])
	i += s

	// -- tail entries
	copy(p[i:], b[e:])

	return &p[0]
}

func putfield(b []byte, f Field, s, e int) *byte {
	name := f.Name()
	var namesz [10]byte
	var namen int
	if useSharedNames {
		num := sstring.Store(name)
		namen = binary.PutUvarint(namesz[:], uint64(num))
	} else {
		namen = binary.PutUvarint(namesz[:], uint64(len(name)))
	}
	value := f.Value()
	kind := value.Kind()
	isdatakind := datakind(kind)
	var data string
	var datasz [10]byte
	var datan int
	if isdatakind {
		data = value.Data()
		datan = binary.PutUvarint(datasz[:], uint64(len(data)))
	}
	var totallen int
	if useSharedNames {
		totallen = s + namen + 1 + (len(b) - e)
	} else {
		totallen = s + namen + len(name) + 1 + +(len(b) - e)
	}
	if isdatakind {
		totallen += datan + len(data)
	}
	var psz [10]byte
	pn := binary.PutUvarint(psz[:], uint64(totallen))
	plen := pn + totallen
	p := make([]byte, plen)

	// copy each component
	i := 0

	// -- header size
	copy(p[i:], psz[:pn])
	i += pn

	// -- head entries
	copy(p[i:], b[:s])
	i += s

	// -- name
	copy(p[i:], namesz[:namen])
	i += namen

	if !useSharedNames {
		copy(p[i:], name)
		i += len(name)
	}

	// -- kind
	p[i] = byte(kind)
	i++

	if isdatakind {
		// -- data
		copy(p[i:], datasz[:datan])
		i += datan

		copy(p[i:], data)
		i += len(data)
	}

	// -- tail entries
	copy(p[i:], b[e:])

	return &p[0]
}

// Get a field from the list. Or returns ZeroField if not found.
func (fields List) Get(name string) Field {
	var isj bool
	var jname string
	var jpath string
	dot := strings.IndexByte(name, '.')
	if dot != -1 {
		isj = true
		jname = name[:dot]
		jpath = name[dot+1:]
	}
	b := ptob(fields.p)
	var i int
	for {
		// read the fname
		var fname string
		x, n := uvarint(b[i:])
		if n == 0 {
			break
		}
		if useSharedNames {
			fname = sstring.Load(x)
			i += n
		} else {
			fname = btoa(b[i+n : i+n+x])
			i += n + x
		}
		kind := Kind(b[i])
		i++
		var data string
		if datakind(kind) {
			x, n = uvarint(b[i:])
			data = btoa(b[i+n : i+n+x])
			i += n + x
		}
		if kind == JSON && isj {
			if jname < fname {
				break
			}
			if fname == jname {
				res := gjson.Get(data, jpath)
				if res.Exists() {
					return bfield(name, Kind(res.Type), res.String())
				}
			}
		} else {
			if name < fname {
				break
			}
			if fname == name {
				return bfield(name, kind, data)
			}
		}
	}
	return ZeroField
}

// Scan each field in list
func (fields List) Scan(iter func(field Field) bool) {
	b := ptob(fields.p)
	var i int
	for {
		// read the fname
		var fname string
		x, n := uvarint(b[i:])
		if n == 0 {
			break
		}
		if useSharedNames {
			fname = sstring.Load(x)
			i += n
		} else {
			fname = btoa(b[i+n : i+n+x])
			i += n + x
		}
		kind := Kind(b[i])
		i++
		var data string
		if datakind(kind) {
			x, n = uvarint(b[i:])
			data = btoa(b[i+n : i+n+x])
			i += n + x
		}
		if !iter(bfield(fname, kind, data)) {
			return
		}
	}
}

// Len return the number of fields in list.
func (fields List) Len() int {
	var count int
	b := ptob(fields.p)
	var i int
	for {
		x, n := uvarint(b[i:])
		if n == 0 {
			break
		}
		if useSharedNames {
			i += n
		} else {
			i += n + x
		}
		isdatakind := datakind(Kind(b[i]))
		i++
		if isdatakind {
			x, n = uvarint(b[i:])
			i += n + x
		}
		count++
	}
	return count
}

// Weight is the number of bytes of the list.
func (fields List) Weight() int {
	if fields.p == nil {
		return 0
	}
	x, n := uvarint(*(*[]byte)(unsafe.Pointer(&bytes{fields.p, 10, 10})))
	return x + n
}

// MakeList returns a field list from an array of fields.
func MakeList(fields []Field) List {
	// TODO: optimize to reduce allocations.
	var list List
	for _, f := range fields {
		list = list.Set(f)
	}
	return list
}

func (fields List) String() string {
	var dst []byte
	dst = append(dst, '{')
	var i int
	fields.Scan(func(f Field) bool {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = gjson.AppendJSONString(dst, f.Name())
		dst = append(dst, ':')
		dst = append(dst, f.Value().JSON()...)
		i++
		return true
	})
	dst = append(dst, '}')
	return string(pretty.UglyInPlace(dst))
}
