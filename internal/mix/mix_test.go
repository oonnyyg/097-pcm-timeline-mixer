package mix

import (
	"math"
	"testing"

	"example.com/pcm-timeline-mixer/internal/wav"
)

func src(id string, channels uint16, samples ...int16) Source {
	return Source{ID: id, File: &wav.File{
		SampleRate: 48000,
		Channels:   channels,
		Samples:    samples,
		NumFrames:  len(samples) / int(channels),
	}}
}

func TestRenderBasic(t *testing.T) {
	s := src("a", 1, 100, 200, 300, 400)
	pcm, frames, nch, rep, err := Render([]Source{s}, []Clip{
		{ID: "c1", SourceID: "a", SourceStart: 1, SourceEnd: 3, OutputStart: 2, Gain: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if frames != 4 || nch != 1 {
		t.Fatalf("frames=%d nch=%d", frames, nch)
	}
	want := []int16{0, 0, 200, 300}
	for i := range want {
		if pcm[i] != want[i] {
			t.Fatalf("pcm=%v want %v", pcm, want)
		}
	}
	if rep.Channels[0].ClippedSamples != 0 || rep.Channels[0].PeakAbs != 300 {
		t.Fatalf("report %+v", rep.Channels[0])
	}
	wantRMS := math.Sqrt((200.0*200 + 300.0*300) / 4)
	if math.Abs(rep.Channels[0].RMS-wantRMS) > 1e-9 {
		t.Fatalf("rms=%v want %v", rep.Channels[0].RMS, wantRMS)
	}
}

func TestRenderSumThenClip(t *testing.T) {
	// Two clips each within range but summing beyond: must clip once.
	s := src("a", 1, 20000, 20000)
	pcm, _, _, rep, err := Render([]Source{s}, []Clip{
		{ID: "c1", SourceID: "a", SourceStart: 0, SourceEnd: 1, OutputStart: 0, Gain: 1},
		{ID: "c2", SourceID: "a", SourceStart: 1, SourceEnd: 2, OutputStart: 0, Gain: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pcm[0] != 32767 || rep.Channels[0].ClippedSamples != 1 {
		t.Fatalf("pcm=%v rep=%+v", pcm, rep.Channels[0])
	}
}

func TestRenderNegativeGainAndRounding(t *testing.T) {
	s := src("a", 1, 5, -5, 3)
	pcm, _, _, _, err := Render([]Source{s}, []Clip{
		{ID: "c1", SourceID: "a", SourceStart: 0, SourceEnd: 3, OutputStart: 0, Gain: 0.5},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 5*0.5=2.5 -> 3 (half away from zero); -5*0.5=-2.5 -> -3; 3*0.5=1.5 -> 2
	want := []int16{3, -3, 2}
	for i := range want {
		if pcm[i] != want[i] {
			t.Fatalf("pcm=%v want %v", pcm, want)
		}
	}
}

func TestRenderStereoIndependentChannels(t *testing.T) {
	s := src("a", 2, 30000, -30000)
	pcm, _, nch, rep, err := Render([]Source{s}, []Clip{
		{ID: "c1", SourceID: "a", SourceStart: 0, SourceEnd: 1, OutputStart: 0, Gain: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if nch != 2 || pcm[0] != 32767 || pcm[1] != -32768 {
		t.Fatalf("pcm=%v", pcm)
	}
	if rep.Channels[0].ClippedSamples != 1 || rep.Channels[1].ClippedSamples != 1 {
		t.Fatalf("rep=%+v", rep)
	}
}

func TestRenderValidation(t *testing.T) {
	s := src("a", 1, 1, 2, 3)
	s2 := src("b", 2, 1, 2)
	cases := map[string]struct {
		sources []Source
		clips   []Clip
	}{
		"no clips":          {[]Source{s}, nil},
		"dup source id":     {[]Source{s, src("a", 1, 9)}, []Clip{{ID: "c", SourceID: "a", SourceStart: 0, SourceEnd: 1}}},
		"dup clip id":       {[]Source{s}, []Clip{{ID: "c", SourceID: "a", SourceStart: 0, SourceEnd: 1}, {ID: "c", SourceID: "a", SourceStart: 0, SourceEnd: 1}}},
		"unknown source":    {[]Source{s}, []Clip{{ID: "c", SourceID: "zz", SourceStart: 0, SourceEnd: 1}}},
		"empty range":       {[]Source{s}, []Clip{{ID: "c", SourceID: "a", SourceStart: 1, SourceEnd: 1}}},
		"negative start":    {[]Source{s}, []Clip{{ID: "c", SourceID: "a", SourceStart: -1, SourceEnd: 1}}},
		"out of bounds":     {[]Source{s}, []Clip{{ID: "c", SourceID: "a", SourceStart: 0, SourceEnd: 99}}},
		"negative output":   {[]Source{s}, []Clip{{ID: "c", SourceID: "a", SourceStart: 0, SourceEnd: 1, OutputStart: -1}}},
		"nan gain":          {[]Source{s}, []Clip{{ID: "c", SourceID: "a", SourceStart: 0, SourceEnd: 1, Gain: math.NaN()}}},
		"inf gain":          {[]Source{s}, []Clip{{ID: "c", SourceID: "a", SourceStart: 0, SourceEnd: 1, Gain: math.Inf(1)}}},
		"format mismatch":   {[]Source{s, s2}, []Clip{{ID: "c", SourceID: "a", SourceStart: 0, SourceEnd: 1}}},
	}
	for name, tc := range cases {
		if _, _, _, _, err := Render(tc.sources, tc.clips); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
