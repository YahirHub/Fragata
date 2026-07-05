package matroska

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"fragata/internal/stream"
)

var (
	idEBML           = []byte{0x1A, 0x45, 0xDF, 0xA3}
	idSegment        = []byte{0x18, 0x53, 0x80, 0x67}
	idInfo           = []byte{0x15, 0x49, 0xA9, 0x66}
	idTracks         = []byte{0x16, 0x54, 0xAE, 0x6B}
	idCluster        = []byte{0x1F, 0x43, 0xB6, 0x75}
	idTimestamp      = []byte{0xE7}
	idSimpleBlock    = []byte{0xA3}
	idTimestampScale = []byte{0x2A, 0xD7, 0xB1}
	idMuxingApp      = []byte{0x4D, 0x80}
	idWritingApp     = []byte{0x57, 0x41}
	idTrackEntry     = []byte{0xAE}
	idTrackNumber    = []byte{0xD7}
	idTrackUID       = []byte{0x73, 0xC5}
	idTrackType      = []byte{0x83}
	idFlagLacing     = []byte{0x9C}
	idCodecID        = []byte{0x86}
	idCodecPrivate   = []byte{0x63, 0xA2}
	idVideo          = []byte{0xE0}
	idPixelWidth     = []byte{0xB0}
	idPixelHeight    = []byte{0xBA}
)

type Writer struct {
	out          io.Writer
	info         stream.Info
	origin       time.Duration
	originSet    bool
	cluster      bytes.Buffer
	clusterStart int64
	clusterSet   bool
	closed       bool
}

func New(out io.Writer, info stream.Info) (*Writer, error) {
	if out == nil {
		return nil, errors.New("writer requerido")
	}
	if info.Width <= 0 || info.Height <= 0 {
		return nil, errors.New("resolución inválida")
	}
	switch info.Codec {
	case "H264":
		if len(info.SPS) < 4 || len(info.PPS) == 0 {
			return nil, errors.New("H.264 requiere SPS y PPS")
		}
	case "H265":
		if len(info.VPS) == 0 || len(info.SPS) == 0 || len(info.PPS) == 0 {
			return nil, errors.New("H.265 requiere VPS, SPS y PPS")
		}
	default:
		return nil, fmt.Errorf("codec no soportado: %s", info.Codec)
	}
	w := &Writer{out: out, info: info}
	if err := w.writeHeader(); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *Writer) writeHeader() error {
	var ebml bytes.Buffer
	writeUint(&ebml, []byte{0x42, 0x86}, 1)
	writeUint(&ebml, []byte{0x42, 0xF7}, 1)
	writeUint(&ebml, []byte{0x42, 0xF2}, 4)
	writeUint(&ebml, []byte{0x42, 0xF3}, 8)
	writeString(&ebml, []byte{0x42, 0x82}, "matroska")
	writeUint(&ebml, []byte{0x42, 0x87}, 4)
	writeUint(&ebml, []byte{0x42, 0x85}, 2)
	if err := writeElement(w.out, idEBML, ebml.Bytes()); err != nil {
		return err
	}
	if _, err := w.out.Write(idSegment); err != nil {
		return err
	}
	if _, err := w.out.Write([]byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}); err != nil {
		return err
	}

	var info bytes.Buffer
	writeUint(&info, idTimestampScale, 1_000_000)
	writeString(&info, idMuxingApp, "Fragata")
	writeString(&info, idWritingApp, "Fragata")
	if err := writeElement(w.out, idInfo, info.Bytes()); err != nil {
		return err
	}

	codecID := "V_MPEG4/ISO/AVC"
	private := avcDecoderConfigurationRecord(w.info.SPS, w.info.PPS)
	if w.info.Codec == "H265" {
		codecID = "V_MPEGH/ISO/HEVC"
		private = hevcDecoderConfigurationRecord(w.info.VPS, w.info.SPS, w.info.PPS)
	}
	var video bytes.Buffer
	writeUint(&video, idPixelWidth, uint64(w.info.Width))
	writeUint(&video, idPixelHeight, uint64(w.info.Height))
	var track bytes.Buffer
	writeUint(&track, idTrackNumber, 1)
	writeUint(&track, idTrackUID, 1)
	writeUint(&track, idTrackType, 1)
	writeUint(&track, idFlagLacing, 0)
	writeString(&track, idCodecID, codecID)
	writeBinary(&track, idCodecPrivate, private)
	writeElementMust(&track, idVideo, video.Bytes())
	var tracks bytes.Buffer
	writeElementMust(&tracks, idTrackEntry, track.Bytes())
	return writeElement(w.out, idTracks, tracks.Bytes())
}

func (w *Writer) WriteAccessUnit(au stream.AccessUnit) error {
	if w.closed {
		return errors.New("writer cerrado")
	}
	if len(au.NALUs) == 0 {
		return nil
	}
	if !w.originSet {
		w.origin = au.PTS
		w.originSet = true
	}
	absoluteMS := (au.PTS - w.origin).Milliseconds()
	if absoluteMS < 0 {
		absoluteMS = 0
	}
	if !w.clusterSet || absoluteMS-w.clusterStart > 5_000 || absoluteMS-w.clusterStart > math.MaxInt16 {
		if err := w.flushCluster(); err != nil {
			return err
		}
		w.clusterStart = absoluteMS
		w.clusterSet = true
		writeUint(&w.cluster, idTimestamp, uint64(w.clusterStart))
	}
	relative := absoluteMS - w.clusterStart
	if relative < math.MinInt16 || relative > math.MaxInt16 {
		return errors.New("timestamp fuera del rango de cluster")
	}
	frame := make([]byte, 0, totalNALSize(au.NALUs))
	for _, nalu := range au.NALUs {
		if len(nalu) == 0 {
			continue
		}
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(nalu)))
		frame = append(frame, length[:]...)
		frame = append(frame, nalu...)
	}
	block := make([]byte, 4, 4+len(frame))
	block[0] = 0x81
	binary.BigEndian.PutUint16(block[1:3], uint16(int16(relative)))
	if au.KeyFrame {
		block[3] = 0x80
	}
	block = append(block, frame...)
	return writeElement(&w.cluster, idSimpleBlock, block)
}

func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.flushCluster()
}

func (w *Writer) flushCluster() error {
	if w.cluster.Len() == 0 {
		return nil
	}
	if err := writeElement(w.out, idCluster, w.cluster.Bytes()); err != nil {
		return err
	}
	w.cluster.Reset()
	return nil
}

func totalNALSize(nalus [][]byte) int {
	total := 0
	for _, nalu := range nalus {
		total += 4 + len(nalu)
	}
	return total
}

func avcDecoderConfigurationRecord(sps, pps []byte) []byte {
	out := []byte{1, sps[1], sps[2], sps[3], 0xFF, 0xE1}
	out = appendUint16(out, len(sps))
	out = append(out, sps...)
	out = append(out, 1)
	out = appendUint16(out, len(pps))
	out = append(out, pps...)
	return out
}

func hevcDecoderConfigurationRecord(vps, sps, pps []byte) []byte {
	// Minimal hvcC record. Parameter arrays carry the authoritative decoder data.
	out := []byte{
		1, 1, // configurationVersion, Main profile
		0, 0, 0, 0, // profile compatibility
		0, 0, 0, 0, 0, 0, // constraint flags
		120,        // level 4.0 fallback
		0xF0, 0x00, // min spatial segmentation
		0xFC,       // parallelism
		0xFD,       // chroma 4:2:0
		0xF8, 0xF8, // bit depth
		0, 0, // average frame rate
		0x0F, // temporal layers + 4-byte NAL lengths
		3,    // arrays
	}
	for _, item := range []struct {
		typ  byte
		nalu []byte
	}{{32, vps}, {33, sps}, {34, pps}} {
		out = append(out, 0x80|item.typ, 0, 1)
		out = appendUint16(out, len(item.nalu))
		out = append(out, item.nalu...)
	}
	return out
}

func appendUint16(dst []byte, value int) []byte {
	return append(dst, byte(value>>8), byte(value))
}

func writeUint(w io.Writer, id []byte, value uint64) {
	size := 1
	for size < 8 && value >= (uint64(1)<<(size*8)) {
		size++
	}
	buf := make([]byte, size)
	for i := size - 1; i >= 0; i-- {
		buf[i] = byte(value)
		value >>= 8
	}
	writeElementMust(w, id, buf)
}

func writeString(w io.Writer, id []byte, value string) { writeElementMust(w, id, []byte(value)) }
func writeBinary(w io.Writer, id, value []byte)        { writeElementMust(w, id, value) }

func writeElementMust(w io.Writer, id, data []byte) {
	if err := writeElement(w, id, data); err != nil {
		panic(err)
	}
}

func writeElement(w io.Writer, id, data []byte) error {
	if _, err := w.Write(id); err != nil {
		return err
	}
	if _, err := w.Write(encodeSize(uint64(len(data)))); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

func encodeSize(value uint64) []byte {
	for n := 1; n <= 8; n++ {
		max := uint64(1)<<(7*n) - 2
		if value <= max {
			out := make([]byte, n)
			v := value
			for i := n - 1; i >= 0; i-- {
				out[i] = byte(v)
				v >>= 8
			}
			out[0] |= byte(1 << (8 - n))
			return out
		}
	}
	panic("elemento EBML demasiado grande")
}
