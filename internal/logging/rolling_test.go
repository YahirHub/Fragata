package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRollingWriterKeepsNewestLinesWithinLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.txt")
	const limit int64 = 512
	writer, err := Open(path, limit)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 100; index++ {
		if _, err := fmt.Fprintf(writer, "line-%03d %s\n", index, bytes.Repeat([]byte{'x'}, 24)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > limit {
		t.Fatalf("log file exceeds limit: %d > %d", info.Size(), limit)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte("line-099")) {
		t.Fatal("newest log line was not preserved")
	}
	if bytes.Contains(raw, []byte("line-000")) {
		t.Fatal("oldest log line was not removed")
	}
}
