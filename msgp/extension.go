package msgp

import (
	"fmt"
	"math"
)

const (
	// Complex64Extension is the extension number used for complex64
	Complex64Extension = 3

	// Complex128Extension is the extension number used for complex128
	Complex128Extension = 4

	// TimeExtension is the extension number used for time.Time
	TimeExtension = 5
)

var (
	// global map of registered extension types
	extensionReg = make(map[int8]func() Extension)
)

// RegisterExtension registers extensions so that they
// can be initialized and returned by methods that
// decode `interface{}` values. This should only
// be called during initialization. f() should return
// a newly-initialized zero value of the extension. Keep in
// mind that extensions 3, 4, and 5 are reserved for
// complex64, complex128, and time.Time, respectively,
// and that MessagePack reserves extension types from -127 to -1.
//
// For example, if you wanted to register a user-defined struct:
//
//  msgp.RegisterExtension(10, func() msgp.Extension { &MyExtension{} })
//
// RegisterExtension will panic if you call it multiple times
// with the same 'typ' argument, or if you use a reserved
// type (3, 4, or 5).
func RegisterExtension(typ int8, f func() Extension) {
	switch typ {
	case 3, 4, 5:
		panic(fmt.Sprint("msgp: forbidden extension type:", typ))
	}
	if _, ok := extensionReg[typ]; ok {
		panic(fmt.Sprint("msgp: RegisterExtension() called with typ", typ, "more than once"))
	}
	extensionReg[typ] = f
}

// ExtensionTypeError is an error type returned
// when there is a mis-match between an extension type
// and the type encoded on the wire
type ExtensionTypeError struct {
	Got  int8
	Want int8
}

// Error implements the error interface
func (e ExtensionTypeError) Error() string {
	return fmt.Sprintf("msgp: error decoding extension: wanted type %d; got type %d", e.Want, e.Got)
}

// Resumable returns 'true' for ExtensionTypeErrors
func (e ExtensionTypeError) Resumable() bool { return true }

func errExt(got int8, wanted int8) error {
	return ExtensionTypeError{Got: got, Want: wanted}
}

// Extension is the interface fulfilled
// by types that want to define their
// own binary encoding.
type Extension interface {
	// ExtensionType should return
	// a int8 that identifies the concrete
	// type of the extension. (Types <0 are
	// officially reserved by the MessagePack
	// specifications.)
	ExtensionType() int8

	// Len should return the length
	// of the data to be encoded
	Len() int

	// MarshalBinaryTo should copy
	// the data into the supplied slice,
	// assuming that the slice has length Len()
	MarshalBinaryTo([]byte) error

	UnmarshalBinary([]byte) error
}

// RawExtension implements the Extension interface
type RawExtension struct {
	Type int8
	Data []byte
}

// ExtensionType implements Extension.ExtensionType, and returns r.Type
func (r *RawExtension) ExtensionType() int8 { return r.Type }

// Len implements Extension.Len, and returns len(r.Data)
func (r *RawExtension) Len() int { return len(r.Data) }

// MarshalBinaryTo implements Extension.MarshalBinaryTo,
// and returns a copy of r.Data
func (r *RawExtension) MarshalBinaryTo(d []byte) error {
	copy(d, r.Data)
	return nil
}

// UnmarshalBinary implements Extension.UnmarshalBinary,
// and sets r.Data to the contents of the provided slice
func (r *RawExtension) UnmarshalBinary(b []byte) error {
	if cap(r.Data) >= len(b) {
		r.Data = r.Data[0:len(b)]
	} else {
		r.Data = make([]byte, len(b))
	}
	copy(r.Data, b)
	return nil
}

// WriteExtension writes an extension type to the writer
func (mw *Writer) WriteExtension(e Extension) error {
	l := e.Len()
	var err error
	switch l {
	case 0:
		o, err := mw.require(3)
		if err != nil {
			return err
		}
		mw.buf[o] = mext8
		mw.buf[o+1] = 0
		mw.buf[o+2] = byte(e.ExtensionType())
	case 1:
		mw.buf = append(mw.buf, mfixext1, byte(e.ExtensionType()))
	case 2:
		mw.buf = append(mw.buf, mfixext2, byte(e.ExtensionType()))
	case 4:
		mw.buf = append(mw.buf, mfixext4, byte(e.ExtensionType()))
	case 8:
		mw.buf = append(mw.buf, mfixext8, byte(e.ExtensionType()))
	case 16:
		mw.buf = append(mw.buf, mfixext16, byte(e.ExtensionType()))
	default:
		switch {
		case l < math.MaxUint8:
			o, err := mw.require(3)
			if err != nil {
				return err
			}
			mw.buf[o] = mext8
			mw.buf[o+1] = byte(uint8(l))
			mw.buf[o+2] = byte(e.ExtensionType())
		case l < math.MaxUint16:
			o, err := mw.require(4)
			if err != nil {
				return err
			}
			mw.buf[o] = mext16
			big.PutUint16(mw.buf[o+1:], uint16(l))
			mw.buf[3] = byte(e.ExtensionType())
		default:
			o, err := mw.require(6)
			if err != nil {
				return err
			}
			mw.buf[o] = mext32
			big.PutUint32(mw.buf[o+1:], uint32(l))
			mw.buf[5] = byte(e.ExtensionType())
		}
	}
	o, err := mw.require(l)
	if err != nil {
		return err
	}
	return e.MarshalBinaryTo(mw.buf[o:])
}

// peek at the extension type, assuming the next
// kind to be read is Extension
func (m *Reader) peekExtensionType() (int8, error) {
	p, err := m.r.Peek(2)
	if err != nil {
		return 0, err
	}
	switch p[0] {
	case mfixext1, mfixext2, mfixext4, mfixext8, mfixext16:
		return int8(p[1]), nil
	case mext8:
		p, err = m.r.Peek(3)
		if err != nil {
			return 0, err
		}
		return int8(p[2]), nil
	case mext16:
		p, err = m.r.Peek(4)
		if err != nil {
			return 0, err
		}
		return int8(p[3]), nil
	case mext32:
		p, err = m.r.Peek(6)
		if err != nil {
			return 0, err
		}
		return int8(p[5]), nil
	default:
		return 0, TypeError{Method: ExtensionType, Encoded: getType(p[0])}
	}
}

// peekExtension peeks at the extension encoding type
// (must guarantee at least 3 bytes in 'b')
func peekExtension(b []byte) (int8, error) {
	switch b[0] {
	case mfixext1, mfixext2, mfixext4, mfixext8, mfixext16:
		return int8(b[1]), nil
	case mext8:
		return int8(b[2]), nil
	case mext16:
		if len(b) < 4 {
			return 0, ErrShortBytes
		}
		return int8(b[3]), nil
	case mext32:
		if len(b) < 5 {
			return 0, ErrShortBytes
		}
		return int8(b[5]), nil
	default:
		return 0, InvalidPrefixError(b[0])
	}
}

// ReadExtension reads the next object from the reader
// as an extension. ReadExtension will fail if the next
// object in the stream is not an extension, or if
// e.Type() is not the same as the wire type.
func (m *Reader) ReadExtension(e Extension) (err error) {
	var p []byte
	p, err = m.r.Peek(2)
	if err != nil {
		return
	}
	lead := p[0]
	var read int
	var off int
	switch lead {
	case mfixext1:
		if int8(p[1]) != e.ExtensionType() {
			err = errExt(int8(p[1]), e.ExtensionType())
			return
		}
		p, err = m.r.Peek(3)
		if err != nil {
			return
		}
		err = e.UnmarshalBinary(p[2:])
		if err == nil {
			_, err = m.r.Skip(3)
		}
		return

	case mfixext2:
		if int8(p[1]) != e.ExtensionType() {
			err = errExt(int8(p[1]), e.ExtensionType())
			return
		}
		p, err = m.r.Peek(4)
		if err != nil {
			return
		}
		err = e.UnmarshalBinary(p[2:])
		if err == nil {
			_, err = m.r.Skip(4)
		}
		return

	case mfixext4:
		if int8(p[1]) != e.ExtensionType() {
			err = errExt(int8(p[1]), e.ExtensionType())
			return
		}
		p, err = m.r.Peek(6)
		if err != nil {
			return
		}
		err = e.UnmarshalBinary(p[2:])
		if err == nil {
			_, err = m.r.Skip(6)
		}
		return

	case mfixext8:
		if int8(p[1]) != e.ExtensionType() {
			err = errExt(int8(p[1]), e.ExtensionType())
			return
		}
		p, err = m.r.Peek(10)
		if err != nil {
			return
		}
		err = e.UnmarshalBinary(p[2:])
		if err == nil {
			_, err = m.r.Skip(10)
		}
		return

	case mfixext16:
		if int8(p[1]) != e.ExtensionType() {
			err = errExt(int8(p[1]), e.ExtensionType())
			return
		}
		p, err = m.r.Peek(18)
		if err != nil {
			return
		}
		err = e.UnmarshalBinary(p[2:])
		if err == nil {
			_, err = m.r.Skip(18)
		}
		return

	case mext8:
		p, err = m.r.Peek(3)
		if err != nil {
			return
		}
		if int8(p[2]) != e.ExtensionType() {
			err = errExt(int8(p[2]), e.ExtensionType())
			return
		}
		read = int(uint8(p[1]))
		off = 3

	case mext16:
		p, err = m.r.Peek(4)
		if err != nil {
			return
		}
		if int8(p[3]) != e.ExtensionType() {
			err = errExt(int8(p[3]), e.ExtensionType())
			return
		}
		read = int(big.Uint16(p[1:]))
		off = 4

	case mext32:
		p, err = m.r.Peek(6)
		if err != nil {
			return
		}
		if int8(p[5]) != e.ExtensionType() {
			err = errExt(int8(p[5]), e.ExtensionType())
			return
		}
		read = int(big.Uint32(p[1:]))
		off = 6

	default:
		err = TypeError{Method: ExtensionType, Encoded: getType(lead)}
		return
	}

	p, err = m.r.Peek(read + off)
	if err != nil {
		return
	}
	err = e.UnmarshalBinary(p[off:])
	if err == nil {
		_, err = m.r.Skip(read + off)
	}
	return
}

// AppendExtension appends a MessagePack extension to the provided slice
func AppendExtension(b []byte, e Extension) ([]byte, error) {
	l := e.Len()
	o, n := ensure(b, ExtensionPrefixSize+l)
	switch l {
	case 0:
		o[n] = mext8
		o[n+1] = 0
		o[n+2] = byte(e.ExtensionType())
		return o[:n+3], nil
	case 1:
		o[n] = mfixext1
		o[n+1] = byte(e.ExtensionType())
		n += 2
	case 2:
		o[n] = mfixext2
		o[n+1] = byte(e.ExtensionType())
		n += 2
	case 4:
		o[n] = mfixext4
		o[n+1] = byte(e.ExtensionType())
		n += 2
	case 8:
		o[n] = mfixext8
		o[n+1] = byte(e.ExtensionType())
		n += 2
	case 16:
		o[n] = mfixext16
		o[n+1] = byte(e.ExtensionType())
		n += 2
	}
	switch {
	case l < math.MaxUint8:
		o[n] = mext8
		o[n+1] = byte(uint8(l))
		o[n+2] = byte(e.ExtensionType())
		n += 3
	case l < math.MaxUint16:
		o[n] = mext16
		big.PutUint16(o[n+1:], uint16(l))
		o[n+3] = byte(e.ExtensionType())
		n += 4
	default:
		o[n] = mext32
		big.PutUint32(o[n+1:], uint32(l))
		o[n+5] = byte(e.ExtensionType())
		n += 6
	}
	return o[:n+l], e.MarshalBinaryTo(o[n : n+l])
}

// ReadExtensionBytes reads an extension from 'b' into 'e'
// and returns any remaining bytes.
// Possible errors:
// - ErrShortBytes ('b' not long enough)
// - ExtensionTypeErorr{} (wire type not the same as e.Type())
// - TypeErorr{} (next object not an extension)
// - An umarshal error returned from e.UnmarshalBinary
func ReadExtensionBytes(b []byte, e Extension) ([]byte, error) {
	l := len(b)
	if l < 3 {
		return b, ErrShortBytes
	}
	lead := b[0]
	var (
		sz  int // size of 'data'
		off int // offset of 'data'
		typ int8
	)
	switch lead {
	case mfixext1:
		typ = int8(b[1])
		sz = 1
		off = 2
	case mfixext2:
		typ = int8(b[1])
		sz = 2
		off = 2
	case mfixext4:
		typ = int8(b[1])
		sz = 4
		off = 2
	case mfixext8:
		typ = int8(b[1])
		sz = 8
		off = 2
	case mfixext16:
		typ = int8(b[1])
		sz = 16
		off = 2
	case mext8:
		sz = int(uint8(b[1]))
		typ = int8(b[2])
		off = 3
		if sz == 0 {
			return b[3:], e.UnmarshalBinary(b[3:3])
		}
	case mext16:
		if l < 4 {
			return b, ErrShortBytes
		}
		sz = int(big.Uint16(b[1:]))
		typ = int8(b[3])
		off = 4
	case mext32:
		if l < 6 {
			return b, ErrShortBytes
		}
		sz = int(big.Uint32(b[1:]))
		typ = int8(b[5])
		off = 6
	default:
		return b, TypeError{Method: ExtensionType, Encoded: getType(lead)}
	}

	if typ != e.ExtensionType() {
		return b, errExt(typ, e.ExtensionType())
	}

	// the data of the extension starts
	// at 'off' and is 'sz' bytes long
	if len(b[off:]) < sz {
		return b, ErrShortBytes
	}
	return b[off+sz:], e.UnmarshalBinary(b[off : off+sz])
}
