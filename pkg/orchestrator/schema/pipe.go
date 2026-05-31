package schema

import (
	"io"
	"sync"
)

// StreamReader is a read-only stream of values.
type StreamReader struct {
	ch   chan streamItem
	once sync.Once
}

type streamItem struct {
	value interface{}
	err   error
}

// Recv reads the next value from the stream. Returns io.EOF when closed.
func (sr *StreamReader) Recv() (interface{}, error) {
	item, ok := <-sr.ch
	if !ok {
		return nil, io.EOF
	}
	return item.value, item.err
}

// StreamWriter is the write side of a pipe.
type StreamWriter struct {
	ch   chan streamItem
	once sync.Once
}

// Send writes a value to the stream.
func (sw *StreamWriter) Send(value interface{}, err error) {
	sw.ch <- streamItem{value: value, err: err}
}

// Close closes the stream. Readers receive io.EOF after draining.
func (sw *StreamWriter) Close() {
	sw.once.Do(func() { close(sw.ch) })
}

// Pipe creates a connected pair of StreamReader and StreamWriter.
func Pipe(bufSize int) (*StreamWriter, *StreamReader) {
	ch := make(chan streamItem, bufSize)
	return &StreamWriter{ch: ch}, &StreamReader{ch: ch}
}

// StreamReaderFromArray creates a StreamReader pre-loaded with values.
func StreamReaderFromArray(values []interface{}) *StreamReader {
	sw, sr := Pipe(len(values))
	for _, v := range values {
		sw.Send(v, nil)
	}
	sw.Close()
	return sr
}
