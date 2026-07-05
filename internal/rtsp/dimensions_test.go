package rtsp

import "testing"

func TestH264Dimensions(t *testing.T) {
	bits := &testBits{}
	bits.writeBits(66, 8) // baseline
	bits.writeBits(0, 8)  // constraints
	bits.writeBits(30, 8) // level
	bits.writeUE(0)       // SPS id
	bits.writeUE(0)       // log2_max_frame_num_minus4
	bits.writeUE(0)       // pic_order_cnt_type
	bits.writeUE(0)       // log2_max_pic_order_cnt_lsb_minus4
	bits.writeUE(1)       // max_num_ref_frames
	bits.writeBit(0)      // gaps allowed
	bits.writeUE(79)      // 80 macroblocks = 1280
	bits.writeUE(44)      // 45 macroblocks = 720
	bits.writeBit(1)      // frame_mbs_only
	bits.writeBit(1)      // direct_8x8
	bits.writeBit(0)      // no crop

	sps := append([]byte{0x67}, bits.bytes()...)
	width, height, err := h264Dimensions(sps)
	if err != nil {
		t.Fatal(err)
	}
	if width != 1280 || height != 720 {
		t.Fatalf("got %dx%d", width, height)
	}
}

func TestH265Dimensions(t *testing.T) {
	bits := &testBits{}
	bits.writeBits(0, 4)  // VPS id
	bits.writeBits(0, 3)  // max sublayers
	bits.writeBit(1)      // temporal nesting
	bits.writeBits(0, 96) // profile tier level
	bits.writeUE(0)       // SPS id
	bits.writeUE(1)       // 4:2:0
	bits.writeUE(2304)
	bits.writeUE(1296)
	bits.writeBit(0) // no conformance window

	sps := append([]byte{0x42, 0x01}, bits.bytes()...)
	width, height, err := h265Dimensions(sps)
	if err != nil {
		t.Fatal(err)
	}
	if width != 2304 || height != 1296 {
		t.Fatalf("got %dx%d", width, height)
	}
}

type testBits struct {
	values []byte
	count  int
}

func (w *testBits) writeBit(value uint64) {
	if w.count%8 == 0 {
		w.values = append(w.values, 0)
	}
	if value&1 == 1 {
		w.values[len(w.values)-1] |= 1 << (7 - uint(w.count%8))
	}
	w.count++
}

func (w *testBits) writeBits(value uint64, count int) {
	for index := count - 1; index >= 0; index-- {
		w.writeBit((value >> uint(index)) & 1)
	}
}

func (w *testBits) writeUE(value uint64) {
	codeNum := value + 1
	bits := 0
	for copy := codeNum; copy > 0; copy >>= 1 {
		bits++
	}
	for range bits - 1 {
		w.writeBit(0)
	}
	w.writeBits(codeNum, bits)
}

func (w *testBits) bytes() []byte { return append([]byte(nil), w.values...) }
