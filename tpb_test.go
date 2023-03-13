package throughputbuffer_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"

	"github.com/Jille/throughputbuffer"
)

func TestBuffer(t *testing.T) {
	tests := []struct {
		writeChunks []int
		readChunks  []int
	}{
		{
			writeChunks: []int{16, 32, 64, 2048},
			readChunks:  []int{2048, 64, 32, 16},
		},
		{
			writeChunks: []int{2048},
			readChunks:  []int{1024, 512, 512},
		},
		{
			writeChunks: []int{1024},
			readChunks:  []int{1024},
		},
		{
			writeChunks: []int{1024},
			readChunks:  []int{1023, 1},
		},
	}
	for _, blocksize := range []int{32, 1024} {
		p := throughputbuffer.New(blocksize)
		for _, testReadFrom := range []bool{false, true} {
			for _, tc := range tests {
				t.Run(fmt.Sprintf("writes: %v; reads: %v; chunksize=%d; readFrom=%v", tc.writeChunks, tc.readChunks, blocksize, testReadFrom), func(t *testing.T) {
					b := p.Get()
					testdata := genData(sum(tc.writeChunks))
					if testReadFrom {
						n, err := b.ReadFrom(&chunkedReader{testdata, tc.writeChunks})
						if err != nil {
							t.Errorf("Buffer.ReadFrom failed: %v", err)
						}
						if n != int64(len(testdata)) {
							t.Errorf("Buffer.ReadFrom didn't read all data: read %d; want %d", n, len(testdata))
						}
					} else {
						toWrite := testdata
						for _, w := range tc.writeChunks {
							n, err := b.Write(toWrite[:w])
							if err != nil {
								t.Errorf("Buffer.Write failed: %v", err)
							}
							if n != w {
								t.Errorf("Buffer.Write gave a short write: %d; want %d", n, w)
							}
							toWrite = toWrite[w:]
						}
					}
					if t.Failed() {
						t.FailNow()
					}
					if len(testdata) != b.Len() {
						t.Errorf("Buffer.Len(): %d; want %d", b.Len(), len(testdata))
					}
					{
						var buf bytes.Buffer
						n, err := b.Clone().WriteTo(&buf)
						if err != nil {
							t.Errorf("Buffer.WriteTo failed: %v", err)
						}
						if n != int64(len(testdata)) {
							t.Errorf("Buffer.WriteTo didn't write all data: wrote %d; want %d", n, len(testdata))
						}
						if !bytes.Equal(buf.Bytes(), testdata) {
							t.Errorf("Incorrect data from WriteTo: %s; want %s", buf.Bytes(), testdata)
						}
					}
					{
						offset := 0
						for _, r := range tc.readChunks {
							buf := make([]byte, r)
							n, err := b.Read(buf)
							if n != r {
								t.Errorf("Buffer.Read(): %d; want %d", n, r)
							}
							if err != nil && (err != io.EOF || offset+n != len(testdata)) {
								t.Errorf("Buffer.Read() failed unexpectedly: %v", err)
							}
							if !bytes.Equal(buf[:n], testdata[offset:offset+n]) {
								t.Errorf("Incorrect data from Read: %s; want %s", buf[:n], testdata[offset:offset+n])
							}
							offset += n
						}
					}
					n, err := b.Read(make([]byte, 1))
					if err != io.EOF || n != 0 {
						t.Errorf("Final read didn't return 0, EOF: %d, %v", n, err)
					}
				})
			}
		}
	}
}

func genData(l int) []byte {
	ret := make([]byte, l)
	for i := 0; l > i; i++ {
		ret[i] = byte('a' + (i % 26))
	}
	return ret
}

func sum(ints []int) int {
	var ret int
	for _, i := range ints {
		ret += i
	}
	return ret
}

type chunkedReader struct {
	content []byte
	chunks  []int
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	l := len(c.content)
	if l == 0 {
		return 0, io.EOF
	}
	if len(c.chunks) > 0 {
		l = c.chunks[0]
		if len(p) >= l {
			c.chunks = c.chunks[1:]
		} else {
			c.chunks = append([]int{l - len(p)}, c.chunks[1:]...)
			l = len(p)
		}
	} else if l > len(p) {
		l = len(p)
	}
	n := copy(p, c.content[:l])
	c.content = c.content[n:]
	return n, nil
}
