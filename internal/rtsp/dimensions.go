package rtsp

import "errors"

// videoDimensions extracts the coded display dimensions from an H.264/H.265
// sequence parameter set. It keeps Fragata independent from native codecs and
// avoids writing an arbitrary fallback resolution into Matroska metadata.
func videoDimensions(codec string, sps []byte) (int, int, error) {
	switch codec {
	case "H264":
		return h264Dimensions(sps)
	case "H265":
		return h265Dimensions(sps)
	default:
		return 0, 0, errors.New("codec sin parser de resolución")
	}
}

type bitReader struct {
	data []byte
	pos  int
}

func newBitReader(nalu []byte, headerBytes int) (*bitReader, error) {
	if len(nalu) <= headerBytes {
		return nil, errors.New("SPS vacío")
	}
	rbsp := make([]byte, 0, len(nalu)-headerBytes)
	zeros := 0
	for _, value := range nalu[headerBytes:] {
		if zeros >= 2 && value == 0x03 {
			zeros = 0
			continue
		}
		rbsp = append(rbsp, value)
		if value == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return &bitReader{data: rbsp}, nil
}

func (r *bitReader) readBit() (uint, error) {
	if r.pos >= len(r.data)*8 {
		return 0, errors.New("SPS truncado")
	}
	value := (r.data[r.pos/8] >> (7 - uint(r.pos%8))) & 1
	r.pos++
	return uint(value), nil
}

func (r *bitReader) readBits(count int) (uint64, error) {
	if count < 0 || count > 64 {
		return 0, errors.New("cantidad de bits inválida")
	}
	var value uint64
	for range count {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		value = value<<1 | uint64(bit)
	}
	return value, nil
}

func (r *bitReader) skipBits(count int) error {
	if count < 0 || r.pos+count > len(r.data)*8 {
		return errors.New("SPS truncado")
	}
	r.pos += count
	return nil
}

func (r *bitReader) readUE() (uint64, error) {
	zeros := 0
	for {
		bit, err := r.readBit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			break
		}
		zeros++
		if zeros > 63 {
			return 0, errors.New("Exp-Golomb inválido")
		}
	}
	if zeros == 0 {
		return 0, nil
	}
	remainder, err := r.readBits(zeros)
	if err != nil {
		return 0, err
	}
	return (uint64(1)<<zeros - 1) + remainder, nil
}

func (r *bitReader) readSE() (int64, error) {
	value, err := r.readUE()
	if err != nil {
		return 0, err
	}
	if value%2 == 0 {
		return -int64(value / 2), nil
	}
	return int64((value + 1) / 2), nil
}

func h264Dimensions(sps []byte) (int, int, error) {
	r, err := newBitReader(sps, 1)
	if err != nil {
		return 0, 0, err
	}
	profileRaw, err := r.readBits(8)
	if err != nil {
		return 0, 0, err
	}
	profile := uint(profileRaw)
	if err := r.skipBits(16); err != nil { // constraints + level_idc
		return 0, 0, err
	}
	if _, err := r.readUE(); err != nil { // seq_parameter_set_id
		return 0, 0, err
	}

	chromaFormatIDC := uint64(1)
	if isH264HighProfile(profile) {
		chromaFormatIDC, err = r.readUE()
		if err != nil {
			return 0, 0, err
		}
		if chromaFormatIDC == 3 {
			if _, err := r.readBit(); err != nil { // separate_colour_plane_flag
				return 0, 0, err
			}
		}
		if _, err := r.readUE(); err != nil { // bit_depth_luma_minus8
			return 0, 0, err
		}
		if _, err := r.readUE(); err != nil { // bit_depth_chroma_minus8
			return 0, 0, err
		}
		if _, err := r.readBit(); err != nil { // qpprime_y_zero_transform_bypass_flag
			return 0, 0, err
		}
		scalingPresent, err := r.readBit()
		if err != nil {
			return 0, 0, err
		}
		if scalingPresent == 1 {
			count := 8
			if chromaFormatIDC == 3 {
				count = 12
			}
			for index := 0; index < count; index++ {
				present, err := r.readBit()
				if err != nil {
					return 0, 0, err
				}
				if present == 1 {
					size := 16
					if index >= 6 {
						size = 64
					}
					if err := skipH264ScalingList(r, size); err != nil {
						return 0, 0, err
					}
				}
			}
		}
	}
	if _, err := r.readUE(); err != nil { // log2_max_frame_num_minus4
		return 0, 0, err
	}
	picOrderCntType, err := r.readUE()
	if err != nil {
		return 0, 0, err
	}
	switch picOrderCntType {
	case 0:
		if _, err := r.readUE(); err != nil {
			return 0, 0, err
		}
	case 1:
		if _, err := r.readBit(); err != nil {
			return 0, 0, err
		}
		if _, err := r.readSE(); err != nil {
			return 0, 0, err
		}
		if _, err := r.readSE(); err != nil {
			return 0, 0, err
		}
		cycles, err := r.readUE()
		if err != nil {
			return 0, 0, err
		}
		for range cycles {
			if _, err := r.readSE(); err != nil {
				return 0, 0, err
			}
		}
	}
	if _, err := r.readUE(); err != nil { // max_num_ref_frames
		return 0, 0, err
	}
	if _, err := r.readBit(); err != nil { // gaps_in_frame_num_value_allowed_flag
		return 0, 0, err
	}
	widthMbs, err := r.readUE()
	if err != nil {
		return 0, 0, err
	}
	heightMapUnits, err := r.readUE()
	if err != nil {
		return 0, 0, err
	}
	frameMbsOnly, err := r.readBit()
	if err != nil {
		return 0, 0, err
	}
	if frameMbsOnly == 0 {
		if _, err := r.readBit(); err != nil { // mb_adaptive_frame_field_flag
			return 0, 0, err
		}
	}
	if _, err := r.readBit(); err != nil { // direct_8x8_inference_flag
		return 0, 0, err
	}

	var cropLeft, cropRight, cropTop, cropBottom uint64
	cropping, err := r.readBit()
	if err != nil {
		return 0, 0, err
	}
	if cropping == 1 {
		if cropLeft, err = r.readUE(); err != nil {
			return 0, 0, err
		}
		if cropRight, err = r.readUE(); err != nil {
			return 0, 0, err
		}
		if cropTop, err = r.readUE(); err != nil {
			return 0, 0, err
		}
		if cropBottom, err = r.readUE(); err != nil {
			return 0, 0, err
		}
	}

	width := int((widthMbs + 1) * 16)
	height := int((heightMapUnits + 1) * 16 * uint64(2-frameMbsOnly))
	cropUnitX, cropUnitY := h264CropUnits(chromaFormatIDC, frameMbsOnly)
	width -= int((cropLeft + cropRight) * cropUnitX)
	height -= int((cropTop + cropBottom) * cropUnitY)
	if width <= 0 || height <= 0 || width > 32768 || height > 32768 {
		return 0, 0, errors.New("resolución H.264 inválida")
	}
	return width, height, nil
}

func isH264HighProfile(profile uint) bool {
	switch profile {
	case 44, 83, 86, 100, 110, 118, 122, 128, 134, 135, 138, 139, 144, 244:
		return true
	default:
		return false
	}
}

func skipH264ScalingList(r *bitReader, size int) error {
	lastScale, nextScale := int64(8), int64(8)
	for index := 0; index < size; index++ {
		if nextScale != 0 {
			delta, err := r.readSE()
			if err != nil {
				return err
			}
			nextScale = (lastScale + delta + 256) % 256
		}
		if nextScale != 0 {
			lastScale = nextScale
		}
	}
	return nil
}

func h264CropUnits(chromaFormatIDC uint64, frameMbsOnly uint) (uint64, uint64) {
	if chromaFormatIDC == 0 {
		return 1, uint64(2 - frameMbsOnly)
	}
	subWidthC, subHeightC := uint64(1), uint64(1)
	switch chromaFormatIDC {
	case 1:
		subWidthC, subHeightC = 2, 2
	case 2:
		subWidthC, subHeightC = 2, 1
	}
	return subWidthC, subHeightC * uint64(2-frameMbsOnly)
}

func h265Dimensions(sps []byte) (int, int, error) {
	r, err := newBitReader(sps, 2)
	if err != nil {
		return 0, 0, err
	}
	if err := r.skipBits(4); err != nil { // sps_video_parameter_set_id
		return 0, 0, err
	}
	maxSubLayersRaw, err := r.readBits(3)
	if err != nil {
		return 0, 0, err
	}
	maxSubLayers := int(maxSubLayersRaw)
	if err := r.skipBits(1); err != nil { // temporal_id_nesting_flag
		return 0, 0, err
	}
	if err := skipH265ProfileTierLevel(r, maxSubLayers); err != nil {
		return 0, 0, err
	}
	if _, err := r.readUE(); err != nil { // sps_seq_parameter_set_id
		return 0, 0, err
	}
	chromaFormatIDC, err := r.readUE()
	if err != nil {
		return 0, 0, err
	}
	if chromaFormatIDC > 3 {
		return 0, 0, errors.New("chroma_format_idc H.265 inválido")
	}
	if chromaFormatIDC == 3 {
		if _, err := r.readBit(); err != nil { // separate_colour_plane_flag
			return 0, 0, err
		}
	}
	widthRaw, err := r.readUE()
	if err != nil {
		return 0, 0, err
	}
	heightRaw, err := r.readUE()
	if err != nil {
		return 0, 0, err
	}
	width, height := int(widthRaw), int(heightRaw)
	window, err := r.readBit()
	if err != nil {
		return 0, 0, err
	}
	if window == 1 {
		left, err := r.readUE()
		if err != nil {
			return 0, 0, err
		}
		right, err := r.readUE()
		if err != nil {
			return 0, 0, err
		}
		top, err := r.readUE()
		if err != nil {
			return 0, 0, err
		}
		bottom, err := r.readUE()
		if err != nil {
			return 0, 0, err
		}
		subWidthC, subHeightC := uint64(1), uint64(1)
		switch chromaFormatIDC {
		case 1:
			subWidthC, subHeightC = 2, 2
		case 2:
			subWidthC = 2
		}
		width -= int((left + right) * subWidthC)
		height -= int((top + bottom) * subHeightC)
	}
	if width <= 0 || height <= 0 || width > 32768 || height > 32768 {
		return 0, 0, errors.New("resolución H.265 inválida")
	}
	return width, height, nil
}

func skipH265ProfileTierLevel(r *bitReader, maxSubLayers int) error {
	if err := r.skipBits(96); err != nil { // general profile/compatibility/constraints/level
		return err
	}
	profilePresent := make([]uint, maxSubLayers)
	levelPresent := make([]uint, maxSubLayers)
	for index := range maxSubLayers {
		value, err := r.readBit()
		if err != nil {
			return err
		}
		profilePresent[index] = value
		value, err = r.readBit()
		if err != nil {
			return err
		}
		levelPresent[index] = value
	}
	if maxSubLayers > 0 {
		if err := r.skipBits((8 - maxSubLayers) * 2); err != nil {
			return err
		}
	}
	for index := range maxSubLayers {
		if profilePresent[index] == 1 {
			if err := r.skipBits(88); err != nil {
				return err
			}
		}
		if levelPresent[index] == 1 {
			if err := r.skipBits(8); err != nil {
				return err
			}
		}
	}
	return nil
}
