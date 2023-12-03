//go:build !linux && !illumos

package throughputbuffer

import (
	"io"
)

func (b *Buffer) tryToWritev(w io.Writer) (int64, error) {
	return 0, nil
}
