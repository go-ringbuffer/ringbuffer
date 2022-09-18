package ringbuffer

import (
	"bytes"
	"errors"
	"io"
	"unsafe"

	"gopkg.in/option.v0"
)

var (
	ErrIsFull              = errors.New("ringbuffer is full")
	ErrIsEmpty             = errors.New("ringbuffer is empty")
	ErrAccuqireLock        = errors.New("no lock to accquire")
	ErrOutOfRange          = errors.New("out of range")
	ErrInvalidSliceIndices = errors.New("invalid slice indices")
)

const DefaultSize = 4096

// Buffer is a circular buffer that implement io.ReaderWriter interface.
type Buffer struct {
	buf    []byte
	size   int
	r      int // next position to read
	w      int // next position to write
	isFull bool
}

var _ interface {
	io.ReadWriter
	io.ByteReader
	io.ByteWriter
	io.ReaderFrom
	io.WriterTo
} = (*Buffer)(nil)

// New returns a new RingBuffer whose buffer has the given size.
func New(options ...Option) (b *Buffer) {
	b = option.New(options)
	if b.buf == nil {
		WithSize(DefaultSize)(b)
	}
	return
}

type Option func(*Buffer)

func WithBuffer(buf []byte) Option {
	return func(b *Buffer) {
		b.buf = buf
		b.size = len(buf)
	}
}

func WithSize(size int) Option {
	return func(b *Buffer) {
		b.buf = make([]byte, size)
		b.size = size
	}
}

// Read reads up to len(p) bytes into p. It returns the number of bytes read (0 <= n <= len(p)) and any error
// encountered. Even if Read returns n < len(p), it may use all of p as scratch space during the call. If some data is
// available but not len(p) bytes, Read conventionally returns what is available instead of waiting for more. When Read
// encounters an error or end-of-file condition after successfully reading n > 0 bytes, it returns the number of bytes
// read. It may return the (non-nil) error from the same call or return the error (and n == 0) from a subsequent call.
// Callers should always process the n > 0 bytes returned before considering the error err. Doing so correctly handles
// I/O errors that happen after reading some bytes and also both of the allowed EOF behaviors.
func (b *Buffer) Read(p []byte) (n int, err error) {
	n, _, err = b.ReadUntilFunc(p, nil)
	return
}

// Fill the bytes slice p up to and including the first delimiter byte that satisfies f. n is the number of bytes
// filled. If no delimiter byte is found after scanning all bytes in the buffer, and there's still room to fill in p,
// then io.EOF error is set. If f is nil, it's the same as Read function.
func (b *Buffer) ReadUntilFunc(p []byte, f func(byte) bool) (n int, atDelim bool, err error) {
	var i, bLen int
	bLen = b.Length()
	if bLen > len(p) {
		n = len(p)
	} else {
		n = bLen
	}
	if n == 0 {
		err = io.EOF
		return
	}
	if f != nil {
		for i = 0; i < n; i++ {
			if f(b.buf[(b.r+i)%b.size]) {
				i++
				atDelim = true
				break
			}
		}
		if i == bLen && !atDelim {
			err = io.EOF
		}
		n = i
	}

	if n > 0 {
		if b.r+n <= b.size {
			copy(p, b.buf[b.r:b.r+n])
		} else {
			c1 := b.size - b.r
			copy(p, b.buf[b.r:b.size])
			c2 := n - c1
			copy(p[c1:], b.buf[0:c2])
		}
		b.r = (b.r + n) % b.size
		b.isFull = false
	}
	return
}

// ReadByte reads and returns the next byte from the input or ErrIsEmpty.
func (b *Buffer) ReadByte() (c byte, err error) {
	if b.w == b.r && !b.isFull {
		return 0, ErrIsEmpty
	}
	c = b.buf[b.r]
	b.r++
	if b.r == b.size {
		b.r = 0
	}
	b.isFull = false
	return c, err
}

// Write writes len(p) bytes from p to the underlying buf. It returns the number of bytes written from p (0 <= n <=
// len(p)) and any error encountered that caused the write to stop early. Write returns a non-nil error if it returns n
// < len(p). Write must not modify the slice data, even temporarily.
func (b *Buffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return
	}
	if b.isFull {
		err = ErrIsFull
		return
	}

	if b.w >= b.r {
		n = b.size - b.w + b.r
	} else {
		n = b.r - b.w
	}

	if n > len(p) {
		n = len(p)
	} else if len(p) > n {
		err = ErrIsFull
	}

	if b.w >= b.r {
		c1 := b.size - b.w
		if c1 >= n {
			copy(b.buf[b.w:], p[:n])
		} else {
			copy(b.buf[b.w:], p[:c1])
			copy(b.buf[:n-c1], p[c1:])
		}
	} else {
		copy(b.buf[b.w:b.w+n], p[:n])
	}

	b.w = (b.w + n) % b.size
	if b.w == b.r {
		b.isFull = true
	}
	return
}

// WriteByte writes one byte into buffer, and returns ErrIsFull if buffer is full.
func (b *Buffer) WriteByte(c byte) error {
	err := b.writeByte(c)
	return err
}

func (b *Buffer) writeByte(c byte) error {
	if b.w == b.r && b.isFull {
		return ErrIsFull
	}
	b.buf[b.w] = c
	b.w++

	if b.w == b.size {
		b.w = 0
	}
	if b.w == b.r {
		b.isFull = true
	}
	return nil
}

func (b *Buffer) ReadFrom(r io.Reader) (n int64, err error) {
	var c, i int
	if b.isFull {
		return 0, ErrIsFull
	}

	for {
		if b.w >= b.r {
			c = b.size
		} else {
			c = b.r
		}
		i, err = r.Read(b.buf[b.w:c])
		b.w = (b.w + i) % b.size
		n += int64(i)
		if err != nil {
			if err == io.EOF && n > 0 {
				err = nil
			}
			return
		}
		if i > 0 && b.w == b.r {
			b.isFull = true
			break
		}
	}

	return
}

func (b *Buffer) WriteTo(w io.Writer) (n int64, err error) {
	var c, i int
	if b.w == b.r && !b.isFull {
		return
	}

	for {
		if b.w > b.r {
			c = b.r
		} else {
			c = b.size
		}
		i, err = w.Write(b.buf[b.r:c])
		b.r = (b.r + i) % b.size
		n += int64(i)
		if err != nil {
			return
		}
		if i > 0 && b.isFull {
			b.isFull = false
		}
		if b.w == b.r {
			return
		}
	}
}

// Length return the length of available read bytes.
func (b *Buffer) Length() int {
	if b.w == b.r {
		if b.isFull {
			return b.size
		}
		return 0
	}
	if b.w > b.r {
		return b.w - b.r
	}
	return b.size - b.r + b.w
}

// Capacity returns the size of the underlying buffer.
func (b *Buffer) Capacity() int {
	return b.size
}

// Free returns the length of available bytes to write.
func (b *Buffer) Free() int {
	if b.w == b.r {
		if b.isFull {
			return 0
		}
		return b.size
	}
	if b.w < b.r {
		return b.r - b.w
	}
	return b.size - b.w + b.r
}

// WriteString writes the contents of the string s to buffer, which accepts a slice of bytes.
func (b *Buffer) WriteString(s string) (n int, err error) {
	x := (*[2]uintptr)(unsafe.Pointer(&s))
	h := [3]uintptr{x[0], x[1], x[1]}
	buf := *(*[]byte)(unsafe.Pointer(&h))
	return b.Write(buf)
}

// Bytes returns all available read bytes. It does not move the read pointer and only copy the available data.
func (b *Buffer) Bytes() []byte {
	return b.byteRange(b.r, b.w)
}

func (b *Buffer) CharAt(n int) byte {
	if b.IsEmpty() {
		panic(ErrOutOfRange)
	}
	return b.buf[(b.r+n)%b.size]
}

type ByteRangeOption func(*byteRangeOptions)
type byteRangeOptions struct {
	start int
	end   int
}

func Start(start int) ByteRangeOption {
	return func(o *byteRangeOptions) {
		o.start = start
	}
}

func End(end int) ByteRangeOption {
	return func(o *byteRangeOptions) {
		o.end = end
	}
}

func (b *Buffer) ByteRange(startEnd ...ByteRangeOption) []byte {
	bLen := b.Length()
	opts := option.New(startEnd, func(o *byteRangeOptions) { o.end = bLen })
	if opts.start < 0 {
		opts.start = bLen - opts.start
		if opts.start < 0 {
			panic(ErrOutOfRange)
		}
	}
	if opts.start > bLen {
		panic(ErrOutOfRange)
	}
	if opts.end < 0 {
		opts.end = bLen - opts.end
		if opts.end < 0 {
			panic(ErrOutOfRange)
		}
	}
	if opts.end > bLen {
		panic(ErrOutOfRange)
	}
	if opts.end < opts.start {
		panic(ErrInvalidSliceIndices)
	}
	if opts.start == opts.end {
		return nil
	}
	i := (b.r + opts.start) % b.size
	j := (b.r + opts.end) % b.size
	return b.byteRange(i, j)
}

func (b *Buffer) byteRange(i, j int) []byte {
	if i == j {
		if b.isFull {
			buf := make([]byte, b.size)
			copy(buf, b.buf[i:])
			copy(buf[b.size-i:], b.buf[:j])
			return buf
		}
		return nil
	}
	if i < j {
		buf := make([]byte, j-i)
		copy(buf, b.buf[i:j])
		return buf
	}
	buf := make([]byte, b.size-i+j)
	copy(buf, b.buf[i:b.size])
	copy(buf[b.size-i:], b.buf[:j])
	return buf
}

// IsFull returns this ringbuffer is full.
func (b *Buffer) IsFull() bool {
	return b.isFull
}

// IsEmpty returns this ringbuffer is empty.
func (b *Buffer) IsEmpty() bool {
	return !b.isFull && b.w == b.r
}

// Reset the read pointer and writer pointer to zero.
func (b *Buffer) Reset() {
	b.r = 0
	b.w = 0
	b.isFull = false
}

func (b *Buffer) Equal(p []byte) bool {
	bLen, pLen := b.Length(), len(p)
	if bLen != pLen {
		return false
	}
	if bLen == 0 {
		return true
	}
	if b.w > b.r {
		return bytes.Equal(b.buf[b.r:b.w], p)
	}
	c := b.size - b.r
	if !bytes.Equal(b.buf[b.r:], p[:c]) {
		return false
	}
	return bytes.Equal(b.buf[:b.w], p[c:])
}

func (b *Buffer) HasPrefix(prefix []byte) bool {
	bLen, pLen := b.Length(), len(prefix)
	if bLen < pLen {
		return false
	}
	if pLen == 0 {
		return true
	}
	if b.w > b.r {
		return bytes.HasPrefix(b.buf[b.r:b.w], prefix)
	}
	c := b.size - b.r
	if c > pLen {
		c = pLen
	}
	if !bytes.Equal(b.buf[b.r:b.r+c], prefix[:c]) {
		return false
	}
	if c == pLen {
		return true
	}
	return bytes.HasPrefix(b.buf[:b.w], prefix[c:])
}

// ReadBytes reads until the first occurrence of delim in the input,
// returning a slice containing the data up to and including the delimiter.
// If ReadBytes encounters an error before finding a delimiter,
// it returns the data read before the error and the error itself (often io.EOF).
// ReadBytes returns err != nil if and only if the returned data does not end in
// delim.
func (b *Buffer) ReadBytes(delim byte) (p []byte, err error) {
	if b.w == b.r && !b.isFull {
		return
	}

	var i, j int
	for {
		j = (b.r + i) % b.size
		if i > 0 && j == b.w {
			err = io.EOF
			break
		}
		if b.buf[j] == delim {
			i += 1
			j += 1
			break
		}
		i += 1
	}

	p = make([]byte, i)
	if j > b.r {
		copy(p, b.buf[b.r:j])
	} else {
		copy(p[:b.size-b.r], b.buf[b.r:])
		copy(p[b.size-b.r:], b.buf[:j])
	}

	return
}

// IndexByte returns the index of the first instance of c in b, or -1 if c is not present in b.
func (b *Buffer) IndexByte(c byte) int {
	bLen := b.Length()
	for i := 0; i < bLen; i++ {
		if b.buf[(b.r+i)%b.size] == c {
			return i
		}
	}
	return -1
}

// IndexByteFunc returns the byte index of the first delimiter byte satisfying f(c), or -1 if none do.
func (b *Buffer) IndexByteFunc(f func(c byte) bool) int {
	var c byte
	bLen := b.Length()
	for i := 0; i < bLen; i++ {
		c = b.buf[(b.r+i)%b.size]
		if f(c) {
			return i
		}
	}
	return -1
}

// ReadBytesFunc returns a bytes array up to and including the first delimiter byte satisfying f(c). If ReadBytesFunc
// encounters an error before finding a delimiter, it returns the data read before the error and the error itself (often
// io.EOF). ReadBytesFunc returns err != nil if and only if the returned data does not end in delim.
func (b *Buffer) ReadBytesFunc(f func(c byte) bool) (p []byte, err error) {
	var i int
	bLen := b.Length()
	err = io.EOF
	for i = 0; i < bLen; i++ {
		if f(b.buf[(b.r+i)%b.size]) {
			i++
			err = nil
			break
		}
	}
	if i > 0 {
		p = b.byteRange(b.r, (b.r+i)%b.size)
	}
	return
}

// Consume n number of bytes from the buffer. Moves the read head n bytes forward.
func (b *Buffer) Consume(n int) {
	if n == 0 {
		return
	}
	bLen := b.Length()
	if n > bLen {
		panic(ErrOutOfRange)
	}
	b.r = (b.r + n) % b.size
	if b.isFull {
		b.isFull = false
	}
}
