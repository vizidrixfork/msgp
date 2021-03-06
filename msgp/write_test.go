package msgp

import (
	"bytes"
	"math"
	"math/rand"
	"testing"
	"unsafe"
)

var (
	tint8          = 126                  // cannot be most fix* types
	tint16         = 150                  // cannot be int8
	tint32         = math.MaxInt16 + 100  // cannot be int16
	tint64         = math.MaxInt32 + 100  // cannot be int32
	tuint16 uint32 = 300                  // cannot be uint8
	tuint32 uint32 = math.MaxUint16 + 100 // cannot be uint16
	tuint64 uint64 = math.MaxUint32 + 100 // cannot be uint32
)

func RandBytes(sz int) []byte {
	out := make([]byte, sz)
	for i := range out {
		out[i] = byte(rand.Int63n(math.MaxInt64) % 256)
	}
	return out
}

func TestWriteMapHeader(t *testing.T) {
	tests := []struct {
		Sz       uint32
		Outbytes []byte
	}{
		{0, []byte{mfixmap}},
		{1, []byte{mfixmap | byte(1)}},
		{100, []byte{mmap16, byte(uint16(100) >> 8), byte(uint16(100))}},
		{tuint32,
			[]byte{mmap32,
				byte(tuint32 >> 24),
				byte(tuint32 >> 16),
				byte(tuint32 >> 8),
				byte(tuint32),
			},
		},
	}

	var buf bytes.Buffer
	var err error
	wr := NewWriter(&buf)
	for _, test := range tests {
		buf.Reset()
		err = wr.WriteMapHeader(test.Sz)
		if err != nil {
			t.Error(err)
		}
		err = wr.Flush()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf.Bytes(), test.Outbytes) {
			t.Errorf("Expected bytes %x; got %x", test.Outbytes, buf.Bytes())
		}
	}
}

func TestWriteArrayHeader(t *testing.T) {
	tests := []struct {
		Sz       uint32
		Outbytes []byte
	}{
		{0, []byte{mfixarray}},
		{1, []byte{mfixarray | byte(1)}},
		{tuint16, []byte{marray16, byte(tuint16 >> 8), byte(tuint16)}},
		{tuint32, []byte{marray32, byte(tuint32 >> 24), byte(tuint32 >> 16), byte(tuint32 >> 8), byte(tuint32)}},
	}

	var buf bytes.Buffer
	var err error
	wr := NewWriter(&buf)
	for _, test := range tests {
		buf.Reset()
		err = wr.WriteArrayHeader(test.Sz)
		if err != nil {
			t.Error(err)
		}
		err = wr.Flush()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(buf.Bytes(), test.Outbytes) {
			t.Errorf("Expected bytes %x; got %x", test.Outbytes, buf.Bytes())
		}
	}
}

func TestWriteNil(t *testing.T) {
	var buf bytes.Buffer
	wr := NewWriter(&buf)

	err := wr.WriteNil()
	if err != nil {
		t.Fatal(err)
	}
	err = wr.Flush()
	if err != nil {
		t.Fatal(err)
	}

	bts := buf.Bytes()
	if bts[0] != mnil {
		t.Errorf("Expected %x; wrote %x", mnil, bts[0])
	}
}

func TestWriteFloat64(t *testing.T) {
	var buf bytes.Buffer
	wr := NewWriter(&buf)

	for i := 0; i < 10000; i++ {
		buf.Reset()
		flt := (rand.Float64() - 0.5) * math.MaxFloat64
		err := wr.WriteFloat64(flt)
		if err != nil {
			t.Errorf("Error with %f: %s", flt, err)
		}
		err = wr.Flush()
		if err != nil {
			t.Fatal(err)
		}

		bts := buf.Bytes()

		if bts[0] != mfloat64 {
			t.Errorf("Leading byte was %x and not %x", bts[0], mfloat64)
		}

		if *(*float64)(unsafe.Pointer(&bts[1])) != flt {
			t.Errorf("Value %f came out as %f", flt, *(*float64)(unsafe.Pointer(&bts[1])))
		}
	}
}

func TestWriteFloat32(t *testing.T) {
	var buf bytes.Buffer
	wr := NewWriter(&buf)

	for i := 0; i < 10000; i++ {
		buf.Reset()
		flt := (rand.Float32() - 0.5) * math.MaxFloat32
		err := wr.WriteFloat32(flt)
		if err != nil {
			t.Errorf("Error with %f: %s", flt, err)
		}
		err = wr.Flush()
		if err != nil {
			t.Fatal(err)
		}

		bts := buf.Bytes()

		if bts[0] != mfloat32 {
			t.Errorf("Leading byte was %x and not %x", bts[0], mfloat64)
		}

		if *(*float32)(unsafe.Pointer(&bts[1])) != flt {
			t.Errorf("Value %f came out as %f", flt, *(*float32)(unsafe.Pointer(&bts[1])))
		}
	}
}

func TestWriteInt64(t *testing.T) {
	var buf bytes.Buffer
	wr := NewWriter(&buf)

	for i := 0; i < 10000; i++ {
		buf.Reset()

		num := (rand.Int63n(math.MaxInt64)) - (math.MaxInt64 / 2)

		err := wr.WriteInt64(num)
		if err != nil {
			t.Fatal(err)
		}
		err = wr.Flush()
		if err != nil {
			t.Fatal(err)
		}

		if buf.Len() > 9 {
			t.Errorf("buffer length should be <= 9; it's %d", buf.Len())
		}
	}
}

func TestWriteUint64(t *testing.T) {
	var buf bytes.Buffer
	wr := NewWriter(&buf)

	for i := 0; i < 10000; i++ {
		buf.Reset()

		num := uint64(rand.Int63n(math.MaxInt64))

		err := wr.WriteUint64(num)
		if err != nil {
			t.Fatal(err)
		}
		err = wr.Flush()
		if err != nil {
			t.Fatal(err)
		}
		if buf.Len() > 9 {
			t.Errorf("buffer length should be <= 9; it's %d", buf.Len())
		}
	}

}

func TestWriteBytes(t *testing.T) {
	var buf bytes.Buffer
	wr := NewWriter(&buf)
	sizes := []int{0, 1, 225, int(tuint32)}

	for _, size := range sizes {
		buf.Reset()
		bts := RandBytes(size)

		err := wr.WriteBytes(bts)
		if err != nil {
			t.Fatal(err)
		}

		err = wr.Flush()
		if err != nil {
			t.Fatal(err)
		}

		if buf.Len() < len(bts) {
			t.Errorf("somehow, %d bytes were encoded in %d bytes", len(bts), buf.Len())
		}
	}
}
