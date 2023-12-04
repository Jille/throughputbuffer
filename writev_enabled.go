//go:build linux || illumos

package throughputbuffer

import (
	"io"
	"syscall"

	"golang.org/x/sys/unix"
)

func (b *Buffer) tryToWritev(w io.Writer) (int64, error) {
	if len(b.buffers) <= 1 {
		return 0, nil
	}
	sc, ok := w.(syscall.Conn)
	if !ok {
		return 0, nil
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		return 0, nil
	}
	var ret int64
	bufs := make([][]byte, len(b.buffers))
	defer func() {
		for i := range bufs {
			// Clear the pointer to allow potential garbage collection.
			bufs[i] = nil
		}
	}()
	for len(b.buffers) > 1 {
		var writevErr error
		err = rc.Write(func(fd uintptr) bool {
			for i, c := range b.buffers {
				bufs[i] = c.data
			}
			var n int
			n, writevErr = unix.Writev(int(fd), bufs[:len(b.buffers)])
			if n > 0 {
				b.dropConsumed(n)
				ret += int64(n)
			}
			if writevErr == syscall.EINTR || writevErr == syscall.EAGAIN {
				writevErr = nil
			}
			return len(b.buffers) == 0 || writevErr != nil
		})
		if writevErr != nil {
			return ret, err
		}
		if err != nil {
			return ret, err
		}
	}
	return ret, nil
}
