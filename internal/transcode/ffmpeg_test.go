package transcode

import (
	"bytes"
	"testing"

	"fragata/internal/stream"
	"github.com/pion/rtp"
)

func TestTailBufferKeepsLastBytes(t *testing.T) {
	buffer := &tailBuffer{limit: 8}
	_, _ = buffer.Write([]byte("12345"))
	_, _ = buffer.Write([]byte("67890"))
	if got := buffer.String(); got != "34567890" {
		t.Fatalf("got %q", got)
	}
}

func TestH264RTPAssemblerPublishesCachedKeyframe(t *testing.T) {
	hub := stream.NewHub()
	assembler := newH264RTPAssembler(hub, 1, 1920, 1080)
	sps := []byte{0x67, 0x42, 0xe0, 0x1f}
	pps := []byte{0x68, 0xce, 0x06, 0xe2}
	stap := []byte{0x78, 0, byte(len(sps))}
	stap = append(stap, sps...)
	stap = append(stap, 0, byte(len(pps)))
	stap = append(stap, pps...)
	assembler.Push(&rtp.Packet{Header: rtp.Header{SequenceNumber: 1, Timestamp: 9000}, Payload: stap})
	assembler.Push(&rtp.Packet{Header: rtp.Header{SequenceNumber: 2, Timestamp: 9000, Marker: true}, Payload: []byte{0x65, 0xaa, 0xbb}})

	info := hub.Info()
	if info.Codec != "H264" || info.Width != 1920 || info.Height != 1080 {
		t.Fatalf("unexpected info: %#v", info)
	}
	if !bytes.Equal(info.SPS, sps) || !bytes.Equal(info.PPS, pps) {
		t.Fatalf("parameter sets were not preserved: %#v", info)
	}
	units, unsubscribe := hub.SubscribeAccessUnits(1)
	defer unsubscribe()
	unit := <-units
	if !unit.KeyFrame || len(unit.NALUs) != 3 {
		t.Fatalf("unexpected access unit: %#v", unit)
	}
}

func TestH264RTPAssemblerRebuildsFUA(t *testing.T) {
	hub := stream.NewHub()
	assembler := newH264RTPAssembler(hub, 1, 640, 360)
	assembler.Push(&rtp.Packet{Header: rtp.Header{SequenceNumber: 10, Timestamp: 1234}, Payload: []byte{0x7c, 0x85, 0xaa}})
	assembler.Push(&rtp.Packet{Header: rtp.Header{SequenceNumber: 11, Timestamp: 1234, Marker: true}, Payload: []byte{0x7c, 0x45, 0xbb}})

	units, unsubscribe := hub.SubscribeAccessUnits(1)
	defer unsubscribe()
	unit := <-units
	if !unit.KeyFrame || len(unit.NALUs) != 1 || !bytes.Equal(unit.NALUs[0], []byte{0x65, 0xaa, 0xbb}) {
		t.Fatalf("FU-A was not rebuilt correctly: %#v", unit)
	}
}
