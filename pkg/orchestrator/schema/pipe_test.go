package schema

import (
	"io"
	"testing"
)

func TestPipeSendRecv(t *testing.T) {
	sw, sr := Pipe(2)
	sw.Send("hello", nil)
	sw.Send("world", nil)
	sw.Close()

	v, err := sr.Recv()
	if err != nil || v != "hello" {
		t.Fatalf("expected 'hello', got %v, err=%v", v, err)
	}
	v, err = sr.Recv()
	if err != nil || v != "world" {
		t.Fatalf("expected 'world', got %v, err=%v", v, err)
	}
	_, err = sr.Recv()
	if err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestPipeError(t *testing.T) {
	sw, sr := Pipe(1)
	sw.Send(nil, io.ErrUnexpectedEOF)
	sw.Close()

	_, err := sr.Recv()
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("expected ErrUnexpectedEOF, got %v", err)
	}
}

func TestStreamReaderFromArray(t *testing.T) {
	sr := StreamReaderFromArray([]interface{}{"a", "b"})
	v, _ := sr.Recv()
	if v != "a" {
		t.Fatalf("expected 'a', got %v", v)
	}
	v, _ = sr.Recv()
	if v != "b" {
		t.Fatalf("expected 'b', got %v", v)
	}
}
