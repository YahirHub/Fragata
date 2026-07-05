package matroska

import (
	"bytes"
	"testing"
	"time"

	"fragata/internal/stream"
)

func TestWriterProducesMatroska(t *testing.T) {
	var out bytes.Buffer
	w, err := New(&out, stream.Info{Codec: "H264", Width: 1280, Height: 720, SPS: []byte{0x67, 0x64, 0, 0x1f}, PPS: []byte{0x68, 0xee, 0x3c, 0x80}})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteAccessUnit(stream.AccessUnit{PTS: time.Second, KeyFrame: true, NALUs: [][]byte{{0x65, 1, 2, 3}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	b := out.Bytes()
	if len(b) < 100 || !bytes.Equal(b[:4], idEBML) {
		t.Fatalf("salida MKV inválida: %d bytes", len(b))
	}
	if !bytes.Contains(b, []byte("V_MPEG4/ISO/AVC")) || !bytes.Contains(b, []byte("Fragata")) {
		t.Fatal("faltan metadatos Matroska")
	}
}
