// Package throughputbuffer provides a high throughput indefinitely growing io.ReadWriter.
// It does the minimum amount of copies (1 per read + 1 per write) and never has to move bytes in the buffer.
//
// Memory is freed once read.
package throughputbuffer

import (
	"io"
	"sync"
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
		buffers: make([][]byte, 0, 32),
	}
}

func (p *BufferPool) getByteSlice() []byte {
	return p.pool.Get().([]byte)[:0]
}

// Buffer is a io.ReadWriter that can grow infinitely and does the minimum amount of copies (1 per read + 1 per write) and never has to move bytes.
type Buffer struct {
	parent *BufferPool

	buffers     [][]byte
	readBufHead []byte
}

var _ io.Reader = &Buffer{}
var _ io.Writer = &Buffer{}
var _ io.ReaderFrom = &Buffer{}
var _ io.WriterTo = &Buffer{}

func (b *Buffer) Write(p []byte) (int, error) {
	if len(b.buffers) == 0 {
		b.buffers = append(b.buffers, b.parent.getByteSlice())
	}
	ret := len(p)
	for len(p) > 0 {
		bs := b.buffers[len(b.buffers)-1]
		if len(bs) < cap(bs) {
			target := bs[len(bs):cap(bs)]
			n := copy(target, p)
			b.buffers[len(b.buffers)-1] = bs[:len(bs)+n]
			p = p[n:]
		} else {
			b.buffers = append(b.buffers, b.parent.getByteSlice())
		}
	}
	return ret, nil
}

func (b *Buffer) ReadFrom(r io.Reader) (int64, error) {
	if len(b.buffers) == 0 {
		b.buffers = append(b.buffers, b.parent.getByteSlice())
	}
	var ret int64
	for {
		bs := b.buffers[len(b.buffers)-1]
		if len(bs) < cap(bs) {
			target := bs[len(bs):cap(bs)]
			n, err := r.Read(target)
			b.buffers[len(b.buffers)-1] = bs[:len(bs)+n]
			ret += int64(n)
			if err == io.EOF {
				return ret, nil
			}
			if err != nil {
				return ret, err
			}
		} else {
			b.buffers = append(b.buffers, b.parent.getByteSlice())
		}
	}
}

func (b *Buffer) returnByteSlice(bs []byte) {
	if b.readBufHead != nil {
		b.parent.pool.Put(b.readBufHead)
		b.readBufHead = nil
	} else {
		b.parent.pool.Put(bs)
	}
}

func (b *Buffer) Read(p []byte) (int, error) {
	var ret int
	for len(b.buffers) > 0 {
		bs := b.buffers[0]
		n := copy(p[ret:], bs)
		if n == len(bs) {
			b.returnByteSlice(bs)
			b.buffers[0] = nil
			b.buffers = b.buffers[1:]
		} else {
			if b.readBufHead == nil {
				b.readBufHead = bs
			}
			b.buffers[0] = bs[n:]
		}
		ret += n
		if ret == len(p) {
			return ret, nil
		}
	}
	return ret, io.EOF
}

func (b *Buffer) WriteTo(w io.Writer) (int64, error) {
	var ret int64
	for len(b.buffers) > 0 {
		bs := b.buffers[0]
		n, err := w.Write(bs)
		ret += int64(n)
		if n == len(bs) {
			b.returnByteSlice(bs)
			b.buffers[0] = nil
			b.buffers = b.buffers[1:]
		} else {
			if b.readBufHead == nil {
				b.readBufHead = bs
			}
			b.buffers[0] = bs[n:]
		}
		if err != nil {
			return ret, err
		}
	}
	return ret, nil
}

func (b *Buffer) Len() int {
	var ret int
	for _, bs := range b.buffers {
		ret += len(bs)
	}
	return ret
}

// Bytes consumes all the data and returns it as a byte slice.
func (b *Buffer) Bytes() []byte {
	ret := make([]byte, 0, b.Len())
	for i, bs := range b.buffers {
		copy(ret[len(ret):cap(ret)], bs)
		b.returnByteSlice(bs)
		b.buffers[i] = nil
	}
	b.buffers = b.buffers[:0]
	return ret
}
