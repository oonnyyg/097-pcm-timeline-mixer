package mixer

import (
	"errors"
	"fmt"
	"math"
)

// Source names one input WAVE file.
type Source struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// Clip places a half-open source frame range [SrcStart, SrcEnd) on the
// output timeline at OutStart, scaled by Gain.
type Clip struct {
	ID       string  `json:"id"`
	Source   string  `json:"source"`
	SrcStart int64   `json:"src_start"`
	SrcEnd   int64   `json:"src_end"`
	OutStart int64   `json:"out_start"`
	Gain     float64 `json:"gain"`
}

// Task is the parsed job description.
type Task struct {
	Sources []Source `json:"sources"`
	Clips   []Clip   `json:"clips"`
}

// ChannelReport summarizes one output channel.
type ChannelReport struct {
	ClippedSamples int64   `json:"clipped_samples"`
	PeakAbs        int     `json:"peak_abs"`
	RMS            float64 `json:"rms"`
}

// Report is written to report.json.
type Report struct {
	SampleRate int             `json:"sample_rate"`
	Channels   int             `json:"channels"`
	Frames     int64           `json:"frames"`
	PerChannel []ChannelReport `json:"per_channel"`
}

// ValidateTask checks uniqueness and clip ranges against loaded sources.
func ValidateTask(t *Task, waves map[string]*Wave) error {
	if len(t.Clips) == 0 {
		return errors.New("task must contain at least one clip")
	}
	srcIDs := map[string]bool{}
	for _, s := range t.Sources {
		if s.ID == "" {
			return errors.New("source id must not be empty")
		}
		if srcIDs[s.ID] {
			return fmt.Errorf("duplicate source id %q", s.ID)
		}
		srcIDs[s.ID] = true
	}
	clipIDs := map[string]bool{}
	for _, c := range t.Clips {
		if c.ID == "" {
			return errors.New("clip id must not be empty")
		}
		if clipIDs[c.ID] {
			return fmt.Errorf("duplicate clip id %q", c.ID)
		}
		clipIDs[c.ID] = true
		w, ok := waves[c.Source]
		if !ok {
			return fmt.Errorf("clip %q references unknown source %q", c.ID, c.Source)
		}
		if c.SrcStart < 0 || c.SrcEnd < 0 || c.OutStart < 0 {
			return fmt.Errorf("clip %q has negative frame index", c.ID)
		}
		if c.SrcEnd <= c.SrcStart {
			return fmt.Errorf("clip %q has empty source range", c.ID)
		}
		if c.SrcEnd > int64(w.Frames()) {
			return fmt.Errorf("clip %q source range exceeds source length", c.ID)
		}
		if math.IsNaN(c.Gain) || math.IsInf(c.Gain, 0) {
			return fmt.Errorf("clip %q gain must be finite", c.ID)
		}
	}
	return nil
}

// Mix renders the clips onto a single timeline. Accumulation happens in
// float64 per output sample across all clips; only the final sum is rounded
// (half away from zero) and saturated to int16 range.
func Mix(t *Task, waves map[string]*Wave, sampleRate, channels int) (*Wave, *Report, error) {
	var totalFrames int64
	for _, c := range t.Clips {
		if end := c.OutStart + (c.SrcEnd - c.SrcStart); end > totalFrames {
			totalFrames = end
		}
	}
	if totalFrames > math.MaxInt64/int64(channels) {
		return nil, nil, errors.New("output too large")
	}
	acc := make([]float64, totalFrames*int64(channels))
	for _, c := range t.Clips {
		w := waves[c.Source]
		n := c.SrcEnd - c.SrcStart
		for f := int64(0); f < n; f++ {
			srcBase := (c.SrcStart + f) * int64(channels)
			dstBase := (c.OutStart + f) * int64(channels)
			for ch := int64(0); ch < int64(channels); ch++ {
				acc[dstBase+ch] += float64(w.Samples[srcBase+ch]) * c.Gain
			}
		}
	}

	out := &Wave{SampleRate: sampleRate, Channels: channels, Samples: make([]int16, len(acc))}
	rep := &Report{
		SampleRate: sampleRate,
		Channels:   channels,
		Frames:     totalFrames,
		PerChannel: make([]ChannelReport, channels),
	}
	var sumSq []float64
	sumSq = make([]float64, channels)
	for i, v := range acc {
		r := math.Round(v) // half away from zero
		ch := i % channels
		if r > 32767 || r < -32768 {
			rep.PerChannel[ch].ClippedSamples++
		}
		s := int16(max(-32768, min(32767, int64(r))))
		out.Samples[i] = s
		abs := int(s)
		if abs < 0 {
			abs = -abs
		}
		if abs > rep.PerChannel[ch].PeakAbs {
			rep.PerChannel[ch].PeakAbs = abs
		}
		sumSq[ch] += float64(s) * float64(s)
	}
	for ch := 0; ch < channels; ch++ {
		if totalFrames > 0 {
			rep.PerChannel[ch].RMS = math.Sqrt(sumSq[ch] / float64(totalFrames))
		}
	}
	return out, rep, nil
}
