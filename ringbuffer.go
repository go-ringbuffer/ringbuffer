package ringbuffer

import (
	"bytes"
	"errors"
	"io"
	"unsafe"
)

var (
	ErrTooManyDataToWrite = errors.New("too many data to write")
	ErrIsFull             = errors.New("ringbuffer is full")
	ErrIsEmpty            = errors.New("ringbuffer is empty")
	ErrAccuqireLock       = errors.New("no lock to accquire")
)

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
	io.ReaderFrom
	io.WriterTo
} = (*Buffer)(nil)

// New returns a new RingBuffer whose buffer has the given size.
func New(size int) *Buffer {
	return &Buffer{
		buf:  make([]byte, size),
		size: size,
	}
}

// Read reads up to len(p) bytes into p. It returns the number of bytes read (0 <= n <= len(p)) and any error encountered. Even if Read returns n < len(p), it may use all of p as scratch space during the call. If some data is available but not len(p) bytes, Read conventionally returns what is available instead of waiting for more.
// When Read encounters an error or end-of-file condition after successfully reading n > 0 bytes, it returns the number of bytes read. It may return the (non-nil) error from the same call or return the error (and n == 0) from a subsequent call.
// Callers should always process the n > 0 bytes returned before considering the error err. Doing so correctly handles I/O errors that happen after reading some bytes and also both of the allowed EOF behaviors.
func (b *Buffer) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.w == b.r && !b.isFull {
		return 0, ErrIsEmpty
	}

	if b.w > b.r {
		n = b.w - b.r
		if n > len(p) {
			n = len(p)
		}
		copy(p, b.buf[b.r:b.r+n])
		b.r = (b.r + n) % b.size
		return
	}

	n = b.size - b.r + b.w
	if n > len(p) {
		n = len(p)
	}

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

	return n, err
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

// Write writes len(p) bytes from p to the underlying buf.
// It returns the number of bytes written from p (0 <= n <= len(p)) and any error encountered that caused the write to stop early.
// Write returns a non-nil error if it returns n < len(p).
// Write must not modify the slice data, even temporarily.
func (b *Buffer) Write(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.isFull {
		return 0, ErrIsFull
	}

	var avail int
	if b.w >= b.r {
		avail = b.size - b.w + b.r
	} else {
		avail = b.r - b.w
	}

	if len(p) > avail {
		err = ErrTooManyDataToWrite
		p = p[:avail]
	}
	n = len(p)

	if b.w >= b.r {
		c1 := b.size - b.w
		if c1 >= n {
			copy(b.buf[b.w:], p)
			b.w += n
		} else {
			copy(b.buf[b.w:], p[:c1])
			c2 := n - c1
			copy(b.buf[0:], p[c1:])
			b.w = c2
		}
	} else {
		copy(b.buf[b.w:], p)
		b.w += n
	}

	if b.w == b.size {
		b.w = 0
	}
	if b.w == b.r {
		b.isFull = true
	}

	return n, err
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
			break
		}
	}

	b.isFull = true
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
	if b.w == b.r {
		if b.isFull {
			buf := make([]byte, b.size)
			copy(buf, b.buf[b.r:])
			copy(buf[b.size-b.r:], b.buf[:b.w])
			return buf
		}
		return nil
	}
	if b.w > b.r {
		buf := make([]byte, b.w-b.r)
		copy(buf, b.buf[b.r:b.w])
		return buf
	}
	n := b.size - b.r + b.w
	buf := make([]byte, n)
	if b.r+n < b.size {
		copy(buf, b.buf[b.r:b.r+n])
	} else {
		c1 := b.size - b.r
		copy(buf, b.buf[b.r:b.size])
		c2 := n - c1
		copy(buf[c1:], b.buf[0:c2])
	}
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
