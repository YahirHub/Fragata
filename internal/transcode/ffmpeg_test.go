package transcode

import "testing"

func TestTailBufferKeepsLastBytes(t *testing.T) {
	buffer := &tailBuffer{limit: 8}
	_, _ = buffer.Write([]byte("12345"))
	_, _ = buffer.Write([]byte("67890"))
	if got := buffer.String(); got != "34567890" {
		t.Fatalf("got %q", got)
	}
}
