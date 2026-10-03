package mixer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// Wave holds a decoded 16-bit PCM WAVE file.
// Samples is interleaved by frame: frame f channel c is Samples[f*Channels+c].
type Wave struct {
	SampleRate int
	Channels   int
	Samples    []int16
}

// Frames returns the number of complete sample frames.
func (w *Wave) Frames() int {
	if w.Channels == 0 {
		return 0
	}
	return len(w.Samples) / w.Channels
}

// ReadWave parses a RIFF little-endian WAVE file containing exactly one fmt
// chunk and one data chunk of format-1 16-bit signed PCM with 1 or 2 channels.
func ReadWave(path string) (*Wave, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return ParseWave(data)
}

// ParseWave parses WAVE data from memory.
func ParseWave(data []byte) (*Wave, error) {
	if len(data) < 12 {
		return nil, errors.New("file too small for RIFF header")
	}
	if string(data[0:4]) != "RIFF" {
		return nil, errors.New("missing RIFF magic")
	}
	if string(data[8:12]) != "WAVE" {
		return nil, errors.New("missing WAVE magic")
	}
	riffSize := int64(binary.LittleEndian.Uint32(data[4:8])) + 8
	if riffSize > int64(len(data)) {
		return nil, errors.New("truncated RIFF data")
	}
	body := data[12:riffSize]

	var w *Wave
	haveFmt, haveData := false, false
	var rawData []byte

	pos := 0
	for pos+8 <= len(body) {
		id := string(body[pos : pos+4])
		size := int64(binary.LittleEndian.Uint32(body[pos+4 : pos+8]))
		pos += 8
		if size > int64(len(body)-pos) {
			return nil, fmt.Errorf("chunk %q truncated", id)
		}
		payload := body[pos : pos+int(size)]
		// Chunks are word-aligned: odd sizes carry one pad byte.
		adv := int(size)
		if size%2 == 1 {
			adv++
		}
		if adv > len(body)-pos {
			return nil, fmt.Errorf("chunk %q missing pad byte", id)
		}
		pos += adv

		switch id {
		case "fmt ":
			if haveFmt {
				return nil, errors.New("duplicate fmt chunk")
			}
			haveFmt = true
			parsed, err := parseFmt(payload)
			if err != nil {
				return nil, err
			}
			w = parsed
		case "data":
			if haveData {
				return nil, errors.New("duplicate data chunk")
			}
			haveData = true
			rawData = payload
		default:
			// Unknown chunks are skipped.
		}
	}
	if pos != len(body) {
		return nil, errors.New("trailing garbage after last chunk")
	}
	if !haveFmt {
		return nil, errors.New("missing fmt chunk")
	}
	if !haveData {
		return nil, errors.New("missing data chunk")
	}

	frameBytes := w.Channels * 2
	if len(rawData)%frameBytes != 0 {
		return nil, errors.New("data chunk does not contain whole sample frames")
	}
	w.Samples = make([]int16, len(rawData)/2)
	for i := range w.Samples {
		w.Samples[i] = int16(binary.LittleEndian.Uint16(rawData[i*2:]))
	}
	return w, nil
}

func parseFmt(b []byte) (*Wave, error) {
	if len(b) < 16 {
		return nil, errors.New("fmt chunk too small")
	}
	format := binary.LittleEndian.Uint16(b[0:2])
	if format != 1 {
		return nil, fmt.Errorf("unsupported format code %d (need 1 = PCM)", format)
	}
	channels := int(binary.LittleEndian.Uint16(b[2:4]))
	if channels != 1 && channels != 2 {
		return nil, fmt.Errorf("unsupported channel count %d", channels)
	}
	rate := int(binary.LittleEndian.Uint32(b[4:8]))
	if rate <= 0 {
		return nil, errors.New("invalid sample rate")
	}
	bits := int(binary.LittleEndian.Uint16(b[14:16]))
	if bits != 16 {
		return nil, fmt.Errorf("unsupported bit depth %d (need 16)", bits)
	}
	return &Wave{SampleRate: rate, Channels: channels}, nil
}

// WriteWave encodes w as a canonical RIFF/WAVE 16-bit PCM file.
func WriteWave(path string, w *Wave) error {
	dataLen := len(w.Samples) * 2
	buf := make([]byte, 0, 44+dataLen)
	buf = append(buf, "RIFF"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(36+dataLen))
	buf = append(buf, "WAVE"...)
	buf = append(buf, "fmt "...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)
	buf = binary.LittleEndian.AppendUint16(buf, 1)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(w.Channels))
	buf = binary.LittleEndian.AppendUint32(buf, uint32(w.SampleRate))
	byteRate := uint32(w.SampleRate * w.Channels * 2)
	buf = binary.LittleEndian.AppendUint32(buf, byteRate)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(w.Channels*2))
	buf = binary.LittleEndian.AppendUint16(buf, 16)
	buf = append(buf, "data"...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(dataLen))
	for _, s := range w.Samples {
		buf = binary.LittleEndian.AppendUint16(buf, uint16(s))
	}
	return os.WriteFile(path, buf, 0o644)
}
