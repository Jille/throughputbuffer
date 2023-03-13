// Package throughputbuffer provides a high throughput indefinitely growing io.ReadWriter.
// It does the minimum amount of copies (1 per read + 1 per write) and never has to move bytes in the buffer.
//
// Memory is freed once read.
package throughputbuffer

import (
	"io"
	"sync"
	"sync/atomic"
)

// BufferPool holds a sync.Pool of byte slices and can be used to create new Buffers.
type BufferPool struct {
	pool sync.Pool
}

// New creates a new BufferPool. The blockSize is the size of the []byte slices internally.
// The blocksize should be within a few orders of magnitude of the expected size of your buffers.
// Using a larger blocksize results in more memory being held but unused, a smaller blocksize takes a bit more CPU cycles.
func New(blocksize int) *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() any { return make([]byte, blocksize) },
		},
	}
}

// Get creates a new Buffer using byte slices from this pool.
func (p *BufferPool) Get() *Buffer {
	return &Buffer{
		parent:  p,
		buffers: make([]dataChunk, 0, 32),
	}
}

func (p *BufferPool) getByteSlice() []byte {
	return p.pool.Get().([]byte)[:0]
}

func (p *BufferPool) newDataChunk() dataChunk {
	b := p.getByteSlice()
	return dataChunk{
		data: b,
		head: b,
	}
}

type dataChunk struct {
	// data contains the unread bytes in this buffer. Bytes between the length and capacity can be written to.
	data []byte
	// head is the original start of this buffer. Once this buffer is done, we need this to return it to the pool.
	head []byte
	// refcnt is an optional refcnt (or nil), used only when this buffer was Cloned.
	refcnt *int32
}

// Buffer is a io.ReadWriter that can grow infinitely and does the minimum amount of copies (1 per read + 1 per write) and never has to move bytes.
type Buffer struct {
	parent *BufferPool

	buffers []dataChunk
}

var _ io.Reader = &Buffer{}
var _ io.Writer = &Buffer{}
var _ io.ReaderFrom = &Buffer{}
var _ io.WriterTo = &Buffer{}

// Write the data into the buffer and return len(p), nil. It always returns a nil error.
func (b *Buffer) Write(p []byte) (int, error) {
	if len(b.buffers) == 0 {
		b.buffers = append(b.buffers, b.parent.newDataChunk())
	}
	ret := len(p)
	for len(p) > 0 {
		buf := b.buffers[len(b.buffers)-1]
		if len(buf.data) < cap(buf.data) && (buf.refcnt == nil || atomic.LoadInt32(buf.refcnt) == 1) {
			target := buf.data[len(buf.data):cap(buf.data)]
			n := copy(target, p)
			b.buffers[len(b.buffers)-1].data = buf.data[:len(buf.data)+n]
			p = p[n:]
		} else {
			b.buffers = append(b.buffers, b.parent.newDataChunk())
		}
	}
	return ret, nil
}

// ReadFrom reads all data from r and return the number of bytes read and the error from the reader.
func (b *Buffer) ReadFrom(r io.Reader) (int64, error) {
	if len(b.buffers) == 0 {
		b.buffers = append(b.buffers, b.parent.newDataChunk())
	}
	var ret int64
	for {
		buf := b.buffers[len(b.buffers)-1]
		if len(buf.data) < cap(buf.data) && (buf.refcnt == nil || atomic.LoadInt32(buf.refcnt) == 1) {
			target := buf.data[len(buf.data):cap(buf.data)]
			n, err := r.Read(target)
			b.buffers[len(b.buffers)-1].data = buf.data[:len(buf.data)+n]
			ret += int64(n)
			if err == io.EOF {
				return ret, nil
			}
			if err != nil {
				return ret, err
			}
		} else {
			b.buffers = append(b.buffers, b.parent.newDataChunk())
		}
	}
}

func (b *Buffer) returnDataChunk(buf dataChunk) {
	if buf.refcnt != nil {
		if atomic.AddInt32(buf.refcnt, -1) > 0 {
			return
		}
	}
	b.parent.pool.Put(buf.head)
}

// Read consumes len(p) bytes from the buffer (or less if the buffer is smaller). The only error it can return is io.EOF.
func (b *Buffer) Read(p []byte) (int, error) {
	var ret int
	for len(b.buffers) > 0 {
		buf := b.buffers[0]
		n := copy(p[ret:], buf.data)
		if n == len(buf.data) {
			b.returnDataChunk(buf)
			b.buffers[0] = dataChunk{}
			b.buffers = b.buffers[1:]
		} else {
			b.buffers[0].data = buf.data[n:]
		}
		ret += n
		if ret == len(p) {
			return ret, nil
		}
	}
	return ret, io.EOF
}

// WriteTo calls w.Write() repeatedly with all the data in the buffer. Any returned error is straight from w.Write().
// If an error is returned, the Buffer will have consumed those bytes but is otherwise still usable.
// If no error is returned, the Buffer will be empty after this.
func (b *Buffer) WriteTo(w io.Writer) (int64, error) {
	var ret int64
	for len(b.buffers) > 0 {
		buf := b.buffers[0]
		n, err := w.Write(buf.data)
		ret += int64(n)
		if n == len(buf.data) {
			b.returnDataChunk(buf)
			b.buffers[0] = dataChunk{}
			b.buffers = b.buffers[1:]
		} else {
			b.buffers[0].data = buf.data[n:]
		}
		if err != nil {
			return ret, err
		}
	}
	return ret, nil
}

// Len returns the number of bytes in the buffer.
func (b *Buffer) Len() int {
	var ret int
	for _, buf := range b.buffers {
		ret += len(buf.data)
	}
	return ret
}

// Bytes consumes all the data and returns it as a byte slice.
func (b *Buffer) Bytes() []byte {
	ret := make([]byte, 0, b.Len())
	for i, buf := range b.buffers {
		n := copy(ret[len(ret):cap(ret)], buf.data)
		ret = ret[:len(ret)+n]
		b.returnDataChunk(buf)
		b.buffers[i] = dataChunk{}
	}
	b.buffers = b.buffers[:0]
	return ret
}

// Reset discards the contents back to the pool. The Buffer can be reused after this.
func (b *Buffer) Reset() {
	for i, buf := range b.buffers {
		b.returnDataChunk(buf)
		b.buffers[i] = dataChunk{}
	}
	b.buffers = b.buffers[:0]
}

// Clone returns a copy of this Buffer. Existing data is shared between the two, but they can be used completely independently.
// Reads and writes to any of the clones won't affect the other.
// Clones (and the original) can be cloned again.
func (b *Buffer) Clone() *Buffer {
	for i, buf := range b.buffers {
		if buf.refcnt == nil {
			r := new(int32)
			*r = 2
			b.buffers[i].refcnt = r
		} else {
			atomic.AddInt32(buf.refcnt, 1)
		}
	}
	n := b.parent.Get()
	n.buffers = append(n.buffers, b.buffers...)
	return n
}
