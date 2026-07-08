package livestream

import (
	"bytes"
	"encoding/binary"
	"os/exec"
	"slices"
	"testing"
)

func TestReadMP4Box(t *testing.T) {
	payload := []byte{1, 2, 3, 4}
	box := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
	copy(box[4:8], "moof")
	copy(box[8:], payload)

	kind, got, err := readMP4Box(bytes.NewReader(box))
	if err != nil {
		t.Fatalf("readMP4Box: %v", err)
	}
	if kind != "moof" {
		t.Fatalf("type = %q, want moof", kind)
	}
	if !bytes.Equal(got, box) {
		t.Fatalf("box changed: %x != %x", got, box)
	}
}

func TestMetadataFromInit(t *testing.T) {
	init := append([]byte("prefix-avcC"), 1, 0x42, 0xe0, 0x1f)
	init = append(init, []byte("-mp4a-suffix")...)
	metadata, err := metadataFromInit(init, "transcode")
	if err != nil {
		t.Fatalf("metadataFromInit: %v", err)
	}
	if metadata.MIME != `video/mp4; codecs="avc1.42E01F, mp4a.40.2"` {
		t.Fatalf("mime = %q", metadata.MIME)
	}
	if !metadata.HasAudio || metadata.Mode != "transcode" || metadata.Transport != "http-fmp4" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
}

func TestMetadataWithoutAudio(t *testing.T) {
	init := append([]byte("avcC"), 1, 0x64, 0, 0x29)
	metadata, err := metadataFromInit(init, "copy")
	if err != nil {
		t.Fatalf("metadataFromInit: %v", err)
	}
	if metadata.MIME != `video/mp4; codecs="avc1.640029"` {
		t.Fatalf("mime = %q", metadata.MIME)
	}
	if metadata.HasAudio {
		t.Fatal("audio should be false")
	}
}

func TestFFmpegProducesMSECompatibleInit(t *testing.T) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	cmd := exec.Command(path,
		"-nostdin", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=15",
		"-t", "1",
		"-an",
		"-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
		"-pix_fmt", "yuv420p", "-profile:v", "baseline", "-bf", "0", "-g", "15",
		"-f", "mp4", "-movflags", "+frag_keyframe+empty_moov+default_base_moof+omit_tfhd_offset",
		"pipe:1",
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("ffmpeg cannot start: %v", err)
	}
	var init bytes.Buffer
	foundMoof := false
	for !foundMoof {
		kind, box, readErr := readMP4Box(stdout)
		if readErr != nil {
			t.Fatalf("read ffmpeg mp4: %v", readErr)
		}
		if kind == "moof" {
			foundMoof = true
			break
		}
		_, _ = init.Write(box)
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	metadata, err := metadataFromInit(init.Bytes(), "transcode")
	if err != nil {
		t.Fatalf("metadata from ffmpeg init: %v", err)
	}
	if metadata.MIME == "" || metadata.Transport != "http-fmp4" {
		t.Fatalf("invalid metadata: %+v", metadata)
	}
}

func TestFFmpegArgsUseRTSPTimeoutOption(t *testing.T) {
	args, _ := ffmpegArgs(Source{
		ID:         "camera-test",
		URL:        "rtsp://user:password@192.0.2.10:554/stream",
		VideoCodec: "H265",
	})

	if slices.Contains(args, "-rw_timeout") {
		t.Fatal("ffmpeg args still contain unsupported -rw_timeout")
	}

	timeoutIndex := slices.Index(args, "-timeout")
	if timeoutIndex < 0 || timeoutIndex+1 >= len(args) {
		t.Fatalf("ffmpeg args do not contain -timeout with a value: %v", args)
	}
	if args[timeoutIndex+1] != "15000000" {
		t.Fatalf("timeout value = %q, want 15000000", args[timeoutIndex+1])
	}

	inputIndex := slices.Index(args, "-i")
	if inputIndex < 0 {
		t.Fatalf("ffmpeg args do not contain -i: %v", args)
	}
	if timeoutIndex > inputIndex {
		t.Fatalf("-timeout must be an input option before -i: %v", args)
	}
}
