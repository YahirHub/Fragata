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

func TestWriterIncludesG711AudioTrack(t *testing.T) {
	var out bytes.Buffer
	w, err := NewWithAudio(&out,
		stream.Info{Codec: "H264", Width: 1280, Height: 720, SPS: []byte{0x67, 0x64, 0, 0x1f}, PPS: []byte{0x68, 0xee, 0x3c, 0x80}},
		stream.AudioInfo{Codec: "PCMA", SampleRate: 8000, Channels: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteAccessUnit(stream.AccessUnit{PTS: time.Second, KeyFrame: true, NALUs: [][]byte{{0x65, 1, 2, 3}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteAudio(stream.AudioPacket{PTS: time.Second + 20*time.Millisecond, Payload: []byte{1, 2, 3, 4}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("A_MS/ACM")) {
		t.Fatal("audio track codec metadata is missing")
	}
}

func TestWriterIncludesAACAudioTrack(t *testing.T) {
	var out bytes.Buffer
	w, err := NewWithAudio(&out,
		stream.Info{Codec: "H264", Width: 1280, Height: 720, SPS: []byte{0x67, 0x64, 0, 0x1f}, PPS: []byte{0x68, 0xee, 0x3c, 0x80}},
		stream.AudioInfo{Codec: "AAC", SampleRate: 48000, Channels: 1, CodecPrivate: []byte{0x11, 0x88}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteAccessUnit(stream.AccessUnit{PTS: time.Second, KeyFrame: true, NALUs: [][]byte{{0x65, 1, 2, 3}}}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteAudio(stream.AudioPacket{PTS: time.Second + 20*time.Millisecond, Payload: []byte{1, 2, 3, 4}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("A_AAC")) || !bytes.Contains(out.Bytes(), []byte{0x11, 0x88}) {
		t.Fatal("AAC audio track metadata is missing")
	}
}
