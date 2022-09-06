# throughputbuffer

[![GoDoc](https://godoc.org/github.com/Jille/throughputbuffer?status.svg)](https://godoc.org/github.com/Jille/throughputbuffer)

Package throughputbuffer provides a high throughput indefinitely growing io.ReadWriter, like bytes.Buffer, but optimized for minimal copies.
It does the minimum amount of copies (1 per read + 1 per write) (or fewer through ReadFrom and WriteTo) and never has to move bytes in the buffer.

Byte slices are shared in a BufferPool. All byte slices are of the same length: the size given to New().

Buffers automatically shrink as data is read from them (they return their data to the pool).

```go
package main

import (
	"crypto/rand"

	"github.com/Jille/throughputbuffer"
)

func main() {
	pool := throughputbuffer.New(64 * 1024 * 1024)

	buf1 := pool.Get()
	io.CopyN(buf1, rand.Reader, 1024 * 1024 * 1024)
	buf2 := pool.Get()
	io.Copy(buf2, buf1) // As data is read from buf, the byteslices are freed and reused by buf2.
}
```
