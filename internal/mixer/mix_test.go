package mixer

import (
	"encoding/binary"
	"math"
	"testing"
)

func wavBytes(t *testing.T, rate, channels int, samples []int16, extraChunks ...[]byte) []byte {
	t.Helper()
	var body []byte
	fmtChunk := make([]byte, 16)
	binary.LittleEndian.PutUint16(fmtChunk[0:], 1)
	binary.LittleEndian.PutUint16(fmtChunk[2:], uint16(channels))
	binary.LittleEndian.PutUint32(fmtChunk[4:], uint32(rate))
	binary.LittleEndian.PutUint32(fmtChunk[8:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(fmtChunk[12:], uint16(channels*2))
	binary.LittleEndian.PutUint16(fmtChunk[14:], 16)
	body = appendChunk(body, "fmt ", fmtChunk)
	for _, c := range extraChunks {
		body = append(body, c...)
	}
	data := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(s))
	}
	body = appendChunk(body, "data", data)
	out := append([]byte("RIFF"), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(body)+4))
	out = append(out, "WAVE"...)
	out = append(out, body...)
	return out
}

func appendChunk(dst []byte, id string, payload []byte) []byte {
	dst = append(dst, id...)
	dst = binary.LittleEndian.AppendUint32(dst, uint32(len(payload)))
	dst = append(dst, payload...)
	if len(payload)%2 == 1 {
		dst = append(dst, 0)
	}
	return dst
}

func TestParseWaveSkipsUnknownAndOddChunks(t *testing.T) {
	junk := appendChunk(nil, "JUNK", []byte{1, 2, 3}) // odd size, padded
	data := wavBytes(t, 48000, 1, []int16{100, -200}, junk)
	w, err := ParseWave(data)
	if err != nil {
		t.Fatal(err)
	}
	if w.SampleRate != 48000 || w.Channels != 1 || w.Frames() != 2 {
		t.Fatalf("got %+v frames=%d", w, w.Frames())
	}
	if w.Samples[0] != 100 || w.Samples[1] != -200 {
		t.Fatalf("samples %v", w.Samples)
	}
}

func TestParseWaveRejects(t *testing.T) {
	good := wavBytes(t, 44100, 2, []int16{1, 2, 3, 4})
	// data chunk holding a single byte: not a whole 2-channel frame.
	oddData := wavBytes(t, 44100, 2, nil)
	oddData = append(oddData[:len(oddData)-8], oddData[len(oddData)-8:len(oddData)-4]...)
	binary.LittleEndian.PutUint32(oddData[len(oddData)-4:], 1)
	oddData = append(oddData, 0, 0) // 1 data byte + pad
	binary.LittleEndian.PutUint32(oddData[4:], uint32(len(oddData)-8))
	cases := map[string][]byte{
		"not riff":      append([]byte("RIFX"), good[4:]...),
		"truncated":     good[:len(good)-3],
		"odd data size": oddData,
	}
	// non-PCM format code
	badFmt := wavBytes(t, 44100, 1, []int16{0})
	badFmt[20] = 3
	cases["format 3"] = badFmt
	// duplicate data chunk
	dup := appendChunk(nil, "data", []byte{0, 0})
	withDup := wavBytes(t, 44100, 1, []int16{0}, dup)
	cases["duplicate data"] = withDup
	for name, data := range cases {
		if _, err := ParseWave(data); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func stereoWave() *Wave {
	return &Wave{SampleRate: 44100, Channels: 2, Samples: []int16{
		1000, -1000,
		2000, -2000,
		3000, -3000,
		4000, -4000,
	}}
}

func TestMixAccumulatesBeforeClipping(t *testing.T) {
	w := &Wave{SampleRate: 8000, Channels: 1, Samples: []int16{20000}}
	waves := map[string]*Wave{"a": w}
	task := &Task{Clips: []Clip{
		{ID: "c1", Source: "a", SrcStart: 0, SrcEnd: 1, OutStart: 0, Gain: 1.0},
		{ID: "c2", Source: "a", SrcStart: 0, SrcEnd: 1, OutStart: 0, Gain: 1.0},
		{ID: "c3", Source: "a", SrcStart: 0, SrcEnd: 1, OutStart: 0, Gain: -1.0},
	}}
	out, rep, err := Mix(task, waves, 8000, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Sum is 20000+20000-20000 = 20000: in range, no clipping despite
	// intermediate partial sums exceeding int16.
	if out.Samples[0] != 20000 || rep.PerChannel[0].ClippedSamples != 0 {
		t.Fatalf("got %d clipped=%d", out.Samples[0], rep.PerChannel[0].ClippedSamples)
	}
}

func TestMixRoundingHalfAwayFromZero(t *testing.T) {
	w := &Wave{SampleRate: 8000, Channels: 1, Samples: []int16{1, -1, 3, -3}}
	waves := map[string]*Wave{"a": w}
	task := &Task{Clips: []Clip{
		{ID: "c", Source: "a", SrcStart: 0, SrcEnd: 4, OutStart: 0, Gain: 0.5},
	}}
	out, _, err := Mix(task, waves, 8000, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := []int16{1, -1, 2, -2} // 0.5->1, -0.5->-1, 1.5->2, -1.5->-2
	for i, s := range out.Samples {
		if s != want[i] {
			t.Fatalf("sample %d: got %d want %d", i, s, want[i])
		}
	}
}

func TestMixClipReportAndRMS(t *testing.T) {
	w := &Wave{SampleRate: 8000, Channels: 1, Samples: []int16{30000, 1000}}
	waves := map[string]*Wave{"a": w}
	task := &Task{Clips: []Clip{
		{ID: "c1", Source: "a", SrcStart: 0, SrcEnd: 1, OutStart: 0, Gain: 2.0},
		{ID: "c2", Source: "a", SrcStart: 1, SrcEnd: 2, OutStart: 2, Gain: 1.0},
	}}
	out, rep, err := Mix(task, waves, 8000, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 3 frames: 60000->32767 (clipped), gap 0, 1000.
	if out.Frames() != 3 {
		t.Fatalf("frames %d", out.Frames())
	}
	if out.Samples[0] != 32767 || out.Samples[1] != 0 || out.Samples[2] != 1000 {
		t.Fatalf("samples %v", out.Samples)
	}
	r := rep.PerChannel[0]
	if r.ClippedSamples != 1 || r.PeakAbs != 32767 {
		t.Fatalf("report %+v", r)
	}
	wantRMS := math.Sqrt((32767.0*32767.0 + 0 + 1000.0*1000.0) / 3)
	if math.Abs(r.RMS-wantRMS) > 1e-9 {
		t.Fatalf("rms %v want %v", r.RMS, wantRMS)
	}
}

func TestMixStereoIndependentChannels(t *testing.T) {
	waves := map[string]*Wave{"s": stereoWave()}
	task := &Task{Clips: []Clip{
		{ID: "c", Source: "s", SrcStart: 1, SrcEnd: 3, OutStart: 1, Gain: 1.5},
	}}
	out, rep, err := Mix(task, waves, 44100, 2)
	if err != nil {
		t.Fatal(err)
	}
	// 4 output frames; frames 0 and 3 are silence.
	want := []int16{0, 0, 3000, -3000, 4500, -4500, 0, 0}
	for i, s := range out.Samples {
		if s != want[i] {
			t.Fatalf("sample %d: got %d want %d", i, s, want[i])
		}
	}
	if rep.PerChannel[0].PeakAbs != 4500 || rep.PerChannel[1].PeakAbs != 4500 {
		t.Fatalf("peaks %+v", rep.PerChannel)
	}
}

func TestValidateTask(t *testing.T) {
	waves := map[string]*Wave{"a": {SampleRate: 8000, Channels: 1, Samples: []int16{1, 2, 3}}}
	base := func() *Task {
		return &Task{
			Sources: []Source{{ID: "a", Path: "a.wav"}},
			Clips:   []Clip{{ID: "c", Source: "a", SrcStart: 0, SrcEnd: 2, OutStart: 0, Gain: 1}},
		}
	}
	if err := ValidateTask(base(), waves); err != nil {
		t.Fatal(err)
	}
	bad := map[string]func(*Task){
		"no clips":         func(t *Task) { t.Clips = nil },
		"dup source":       func(t *Task) { t.Sources = append(t.Sources, t.Sources[0]) },
		"dup clip":         func(t *Task) { t.Clips = append(t.Clips, t.Clips[0]) },
		"unknown source":   func(t *Task) { t.Clips[0].Source = "zzz" },
		"empty range":      func(t *Task) { t.Clips[0].SrcEnd = 0 },
		"negative start":   func(t *Task) { t.Clips[0].SrcStart = -1 },
		"negative out":     func(t *Task) { t.Clips[0].OutStart = -1 },
		"out of bounds":    func(t *Task) { t.Clips[0].SrcEnd = 4 },
		"nan gain":         func(t *Task) { t.Clips[0].Gain = math.NaN() },
		"inf gain":         func(t *Task) { t.Clips[0].Gain = math.Inf(1) },
	}
	for name, mutate := range bad {
		task := base()
		mutate(task)
		if err := ValidateTask(task, waves); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
