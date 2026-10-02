package build

import (
	"bytes"
	"crypto/sha256"
	"reflect"

	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// memoKeyGen is an mmdbwriter.KeyGenerator that serialises each distinct
// value instance only once. mmdbwriter asks for a key on every inserted
// prefix; our values are cached per record, so most calls hit the memo.
// Keys are content hashes, so equal content from different instances is
// still deduplicated. The memo keeps a reference to every instance it has
// seen, which prevents the garbage collector from reusing an address for a
// different value.
type memoKeyGen struct {
	buf  bytes.Buffer
	memo map[uintptr]memoEntry
}

type memoEntry struct {
	key  [sha256.Size]byte
	keep mmdbtype.Map
}

func newMemoKeyGen() *memoKeyGen { return &memoKeyGen{memo: map[uintptr]memoEntry{}} }

// Key implements mmdbwriter.KeyGenerator.
func (g *memoKeyGen) Key(v mmdbtype.DataType) ([]byte, error) {
	m, isMap := v.(mmdbtype.Map)
	var ptr uintptr
	if isMap {
		ptr = reflect.ValueOf(m).Pointer()
		if e, ok := g.memo[ptr]; ok {
			return e.key[:], nil
		}
	}
	g.buf.Reset()
	if _, err := v.WriteTo(g); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(g.buf.Bytes())
	if isMap && ptr != 0 {
		g.memo[ptr] = memoEntry{key: sum, keep: m}
	}
	return sum[:], nil
}

func (g *memoKeyGen) Write(p []byte) (int, error)                            { return g.buf.Write(p) }
func (g *memoKeyGen) WriteByte(b byte) error                                 { return g.buf.WriteByte(b) }
func (g *memoKeyGen) WriteString(s string) (int, error)                      { return g.buf.WriteString(s) }
func (g *memoKeyGen) WriteOrWritePointer(t mmdbtype.DataType) (int64, error) { return t.WriteTo(g) }
