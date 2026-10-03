package wav

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func buildWAV(channels uint16, rate uint32, samples []int16, extraChunks ...[]byte) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	b.Write(make([]byte, 4)) // size placeholder
	b.WriteString("WAVE")
	for _, c := range extraChunks {
		b.Write(c)
	}
	b.WriteString("fmt ")
	binary.Write(&b, binary.LittleEndian, uint32(16))
	binary.Write(&b, binary.LittleEndian, uint16(1))
	binary.Write(&b, binary.LittleEndian, channels)
	binary.Write(&b, binary.LittleEndian, rate)
	binary.Write(&b, binary.LittleEndian, rate*uint32(channels)*2)
	binary.Write(&b, binary.LittleEndian, channels*2)
	binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(len(samples)*2))
	for _, s := range samples {
		binary.Write(&b, binary.LittleEndian, s)
	}
	out := b.Bytes()
	binary.LittleEndian.PutUint32(out[4:8], uint32(len(out)-8))
	return out
}

func oddChunk(id string, payload []byte) []byte {
	var b bytes.Buffer
	b.WriteString(id)
	binary.Write(&b, binary.LittleEndian, uint32(len(payload)))
	b.Write(payload)
	if len(payload)%2 == 1 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

func TestParseValid(t *testing.T) {
	data := buildWAV(2, 48000, []int16{1, -2, 300, -400})
	f, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if f.SampleRate != 48000 || f.Channels != 2 || f.NumFrames != 2 {
		t.Fatalf("got %+v", f)
	}
	if f.Samples[3] != -400 {
		t.Fatalf("samples %v", f.Samples)
	}
}

func TestParseSkipsUnknownAndOddChunks(t *testing.T) {
	junk := oddChunk("JUNK", []byte{1, 2, 3}) // odd size, padded
	data := buildWAV(1, 8000, []int16{7}, junk)
	f, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if f.NumFrames != 1 || f.Samples[0] != 7 {
		t.Fatalf("got %+v", f)
	}
}

func TestRejects(t *testing.T) {
	cases := map[string][]byte{
		"not riff":      []byte("RIFX________WAVE"),
		"not wave":      []byte("RIFF________AVI "),
		"truncated":     buildWAV(1, 8000, []int16{1, 2, 3})[:20],
		"partial frame": buildWAV(2, 8000, []int16{1, 2, 3}),
	}
	for name, data := range cases {
		if _, err := Parse(bytes.NewReader(data)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}

	// non-PCM format code
	pcm := buildWAV(1, 8000, []int16{1})
	pcm[20] = 3 // format code 3
	if _, err := Parse(bytes.NewReader(pcm)); err == nil ||
		!strings.Contains(err.Error(), "format code") {
		t.Errorf("format code: got %v", err)
	}

	// 8-bit
	bits := buildWAV(1, 8000, []int16{1})
	binary.LittleEndian.PutUint16(bits[34:36], 8)
	if _, err := Parse(bytes.NewReader(bits)); err == nil {
		t.Error("8-bit: expected error")
	}

	// 4 channels
	ch := buildWAV(1, 8000, []int16{1})
	binary.LittleEndian.PutUint16(ch[22:24], 4)
	if _, err := Parse(bytes.NewReader(ch)); err == nil {
		t.Error("4 channels: expected error")
	}

	// duplicate fmt
	dup := buildWAV(1, 8000, []int16{1})
	fmtChunk := dup[12:36]
	var merged bytes.Buffer
	merged.Write(dup[:12])
	merged.Write(fmtChunk)
	merged.Write(dup[12:])
	if _, err := Parse(bytes.NewReader(merged.Bytes())); err == nil {
		t.Error("duplicate fmt: expected error")
	}
}
