// Package wav parses RIFF little-endian WAVE files containing
// 16-bit signed PCM (format code 1) with one or two channels.
package wav

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// File holds a decoded PCM source.
type File struct {
	SampleRate uint32
	Channels   uint16
	Samples    []int16 // interleaved by frame
	NumFrames  int
}

// Load reads and validates a WAVE file from path.
func Load(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse decodes a RIFF WAVE stream.
func Parse(r io.Reader) (*File, error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return nil, fmt.Errorf("reading RIFF header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" {
		return nil, errors.New("not a RIFF file")
	}
	if string(riff[8:12]) != "WAVE" {
		return nil, errors.New("not a WAVE file")
	}

	var fmtSeen, dataSeen bool
	var sampleRate uint32
	var channels uint16
	var data []byte

	for {
		var hdr [8]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		id := string(hdr[0:4])
		size := binary.LittleEndian.Uint32(hdr[4:8])
		if size > 1<<31 {
			return nil, fmt.Errorf("chunk %q too large", id)
		}
		body := make([]byte, size)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, fmt.Errorf("truncated chunk %q: %w", id, err)
		}
		if size%2 == 1 {
			var pad [1]byte
			if _, err := io.ReadFull(r, pad[:]); err != nil {
				return nil, fmt.Errorf("missing pad byte after odd chunk %q", id)
			}
		}
		switch id {
		case "fmt ":
			if fmtSeen {
				return nil, errors.New("multiple fmt chunks")
			}
			fmtSeen = true
			if len(body) < 16 {
				return nil, errors.New("fmt chunk too small")
			}
			format := binary.LittleEndian.Uint16(body[0:2])
			if format != 1 {
				return nil, fmt.Errorf("unsupported format code %d (want 1, PCM)", format)
			}
			channels = binary.LittleEndian.Uint16(body[2:4])
			if channels != 1 && channels != 2 {
				return nil, fmt.Errorf("unsupported channel count %d", channels)
			}
			sampleRate = binary.LittleEndian.Uint32(body[4:8])
			bits := binary.LittleEndian.Uint16(body[14:16])
			if bits != 16 {
				return nil, fmt.Errorf("unsupported bit depth %d (want 16)", bits)
			}
		case "data":
			if dataSeen {
				return nil, errors.New("multiple data chunks")
			}
			dataSeen = true
			data = body
		}
	}

	if !fmtSeen {
		return nil, errors.New("missing fmt chunk")
	}
	if !dataSeen {
		return nil, errors.New("missing data chunk")
	}

	frameBytes := int(channels) * 2
	if len(data)%frameBytes != 0 {
		return nil, fmt.Errorf("data size %d is not a whole number of frames", len(data))
	}
	numFrames := len(data) / frameBytes
	samples := make([]int16, numFrames*int(channels))
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(data[i*2 : i*2+2]))
	}
	return &File{
		SampleRate: sampleRate,
		Channels:   channels,
		Samples:    samples,
		NumFrames:  numFrames,
	}, nil
}
