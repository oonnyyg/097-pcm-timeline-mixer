// Package mix implements timeline mixing of PCM clips.
package mix

import (
	"errors"
	"fmt"
	"math"

	"example.com/pcm-timeline-mixer/internal/wav"
)

// Source is a named PCM source.
type Source struct {
	ID   string
	File *wav.File
}

// Clip places a frame range of a source on the output timeline.
type Clip struct {
	ID          string
	SourceID    string
	SourceStart int64 // inclusive, in frames
	SourceEnd   int64 // exclusive, in frames
	OutputStart int64 // in frames
	Gain        float64
}

// ChannelReport summarizes one output channel.
type ChannelReport struct {
	ClippedSamples int64   `json:"clipped_samples"`
	PeakAbs        int     `json:"peak_abs"`
	RMS            float64 `json:"rms"`
}

// Report describes the rendered mix.
type Report struct {
	Frames     int64           `json:"frames"`
	SampleRate uint32          `json:"sample_rate"`
	Channels   []ChannelReport `json:"channels"`
}

// Render mixes clips onto a zero-filled timeline and returns the
// interleaved 16-bit PCM plus a per-channel report.
func Render(sources []Source, clips []Clip) ([]int16, int64, int, *Report, error) {
	if len(sources) == 0 {
		return nil, 0, 0, nil, errors.New("no sources")
	}
	if len(clips) == 0 {
		return nil, 0, 0, nil, errors.New("no clips")
	}
	byID := map[string]*wav.File{}
	for _, s := range sources {
		if s.ID == "" {
			return nil, 0, 0, nil, errors.New("source with empty id")
		}
		if _, dup := byID[s.ID]; dup {
			return nil, 0, 0, nil, fmt.Errorf("duplicate source id %q", s.ID)
		}
		byID[s.ID] = s.File
	}
	rate := sources[0].File.SampleRate
	channels := sources[0].File.Channels
	for _, s := range sources[1:] {
		if s.File.SampleRate != rate || s.File.Channels != channels {
			return nil, 0, 0, nil, fmt.Errorf("source %q: sample rate / channels mismatch", s.ID)
		}
	}

	clipIDs := map[string]bool{}
	var totalFrames int64
	for _, c := range clips {
		if c.ID == "" {
			return nil, 0, 0, nil, errors.New("clip with empty id")
		}
		if clipIDs[c.ID] {
			return nil, 0, 0, nil, fmt.Errorf("duplicate clip id %q", c.ID)
		}
		clipIDs[c.ID] = true
		src, ok := byID[c.SourceID]
		if !ok {
			return nil, 0, 0, nil, fmt.Errorf("clip %q: unknown source %q", c.ID, c.SourceID)
		}
		if c.SourceStart < 0 || c.SourceEnd <= c.SourceStart {
			return nil, 0, 0, nil, fmt.Errorf("clip %q: invalid or empty source range [%d,%d)", c.ID, c.SourceStart, c.SourceEnd)
		}
		if c.SourceEnd > int64(src.NumFrames) {
			return nil, 0, 0, nil, fmt.Errorf("clip %q: source range exceeds source length %d", c.ID, src.NumFrames)
		}
		if c.OutputStart < 0 {
			return nil, 0, 0, nil, fmt.Errorf("clip %q: negative output start", c.ID)
		}
		if math.IsNaN(c.Gain) || math.IsInf(c.Gain, 0) {
			return nil, 0, 0, nil, fmt.Errorf("clip %q: gain must be finite", c.ID)
		}
		if end := c.OutputStart + (c.SourceEnd - c.SourceStart); end > totalFrames {
			totalFrames = end
		}
	}

	nch := int(channels)
	acc := make([]float64, totalFrames*int64(nch))
	for _, c := range clips {
		src := byID[c.SourceID]
		frames := c.SourceEnd - c.SourceStart
		for f := int64(0); f < frames; f++ {
			srcBase := (c.SourceStart + f) * int64(nch)
			dstBase := (c.OutputStart + f) * int64(nch)
			for ch := 0; ch < nch; ch++ {
				acc[dstBase+int64(ch)] += float64(src.Samples[srcBase+int64(ch)]) * c.Gain
			}
		}
	}

	out := make([]int16, totalFrames*int64(nch))
	rep := &Report{Frames: totalFrames, SampleRate: rate, Channels: make([]ChannelReport, nch)}
	var sumSq = make([]float64, nch)
	for i := range out {
		ch := i % nch
		v := roundHalfAwayFromZero(acc[i])
		if v > 32767 {
			rep.Channels[ch].ClippedSamples++
			v = 32767
		} else if v < -32768 {
			rep.Channels[ch].ClippedSamples++
			v = -32768
		}
		s := int16(v)
		out[i] = s
		a := int(s)
		if a < 0 {
			a = -a
		}
		if a > rep.Channels[ch].PeakAbs {
			rep.Channels[ch].PeakAbs = a
		}
		sumSq[ch] += float64(s) * float64(s)
	}
	for ch := 0; ch < nch; ch++ {
		rep.Channels[ch].RMS = math.Sqrt(sumSq[ch] / float64(totalFrames))
	}
	return out, totalFrames, nch, rep, nil
}

func roundHalfAwayFromZero(x float64) float64 {
	if x >= 0 {
		return math.Floor(x + 0.5)
	}
	return math.Ceil(x - 0.5)
}
