package mcpbridge

import (
	"bufio"
	"errors"
	"strings"
	"testing"
)

func TestReadFrameRejectsOversizedMessage(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(strings.Repeat("x", 33) + "\n"))
	_, err := readFrame(reader, 32)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}

func TestReadFrameAcceptsBoundedMessage(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("{\"ok\":true}\n"))
	frame, err := readFrame(reader, 32)
	if err != nil {
		t.Fatal(err)
	}
	if string(frame) != "{\"ok\":true}" {
		t.Fatalf("unexpected frame %q", frame)
	}
}
