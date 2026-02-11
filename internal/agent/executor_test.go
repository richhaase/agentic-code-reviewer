package agent

import (
	"testing"
)

func TestCappedBuffer_UnderLimit(t *testing.T) {
	buf := newCappedBuffer(100)
	n, err := buf.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write() = %d, want 5", n)
	}
	if buf.String() != "hello" {
		t.Errorf("String() = %q, want %q", buf.String(), "hello")
	}
}

func TestCappedBuffer_AtLimit(t *testing.T) {
	buf := newCappedBuffer(5)
	buf.Write([]byte("hello"))
	// At limit — further writes silently discarded
	n, err := buf.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 6 {
		t.Errorf("Write() should report full length even when discarded, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("String() = %q, want %q", buf.String(), "hello")
	}
}

func TestCappedBuffer_PartialWrite(t *testing.T) {
	buf := newCappedBuffer(8)
	buf.Write([]byte("hello"))
	// 3 bytes remaining — should write partial but report full length
	n, err := buf.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 6 {
		t.Errorf("Write() should report full length to avoid io.ErrShortWrite, got %d", n)
	}
	if buf.String() != "hello wo" {
		t.Errorf("String() = %q, want %q", buf.String(), "hello wo")
	}
}

func TestCappedBuffer_ZeroLimit(t *testing.T) {
	buf := newCappedBuffer(0)
	n, err := buf.Write([]byte("anything"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 8 {
		t.Errorf("Write() should report full length, got %d", n)
	}
	if buf.String() != "" {
		t.Errorf("String() = %q, want empty", buf.String())
	}
}
