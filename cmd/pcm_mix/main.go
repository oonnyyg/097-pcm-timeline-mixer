// Command pcm_mix renders a PCM mix from a JSON task file.
//
// Usage: go run ./cmd/pcm_mix input.json --out <output-dir>
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"example.com/pcm-timeline-mixer/internal/mix"
	"example.com/pcm-timeline-mixer/internal/wav"
)

type taskFile struct {
	Sources []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"sources"`
	Clips []struct {
		ID          string  `json:"id"`
		Source      string  `json:"source"`
		SourceStart int64   `json:"source_start"`
		SourceEnd   int64   `json:"source_end"`
		OutputStart int64   `json:"output_start"`
		Gain        float64 `json:"gain"`
	} `json:"clips"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pcm_mix:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 3 || args[1] != "--out" {
		return errors.New("usage: pcm_mix <input.json> --out <output-dir>")
	}
	taskPath, outDir := args[0], args[2]

	raw, err := os.ReadFile(taskPath)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var task taskFile
	if err := dec.Decode(&task); err != nil {
		return fmt.Errorf("parsing task file: %w", err)
	}
	baseDir := filepath.Dir(taskPath)

	var sources []mix.Source
	srcPaths := map[string]bool{}
	for _, s := range task.Sources {
		p := filepath.Join(baseDir, s.Path)
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		f, err := wav.Load(p)
		if err != nil {
			return fmt.Errorf("source %q: %w", s.ID, err)
		}
		sources = append(sources, mix.Source{ID: s.ID, File: f})
		srcPaths[abs] = true
	}

	var clips []mix.Clip
	for _, c := range task.Clips {
		clips = append(clips, mix.Clip{
			ID:          c.ID,
			SourceID:    c.Source,
			SourceStart: c.SourceStart,
			SourceEnd:   c.SourceEnd,
			OutputStart: c.OutputStart,
			Gain:        c.Gain,
		})
	}

	pcm, frames, nch, report, err := mix.Render(sources, clips)
	if err != nil {
		return err
	}
	rate := sources[0].File.SampleRate

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	wavPath, _ := filepath.Abs(filepath.Join(outDir, "mix.wav"))
	repPath, _ := filepath.Abs(filepath.Join(outDir, "report.json"))
	if srcPaths[wavPath] || srcPaths[repPath] {
		return errors.New("output would overwrite a source file")
	}

	if err := writeWAV(wavPath, pcm, rate, uint16(nch)); err != nil {
		return err
	}
	repJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(repPath, append(repJSON, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d frames, %d ch, %d Hz) and %s\n", wavPath, frames, nch, rate, repPath)
	return nil
}

func writeWAV(path string, samples []int16, rate uint32, channels uint16) error {
	dataSize := uint32(len(samples) * 2)
	if uint64(len(samples))*2 > math.MaxUint32 {
		return errors.New("output too large for RIFF")
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 0, 44+dataSize)
	buf = append(buf, "RIFF"...)
	buf = binary.LittleEndian.AppendUint32(buf, 36+dataSize)
	buf = append(buf, "WAVE"...)
	buf = append(buf, "fmt "...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)
	buf = binary.LittleEndian.AppendUint16(buf, 1)
	buf = binary.LittleEndian.AppendUint16(buf, channels)
	buf = binary.LittleEndian.AppendUint32(buf, rate)
	byteRate := rate * uint32(channels) * 2
	buf = binary.LittleEndian.AppendUint32(buf, byteRate)
	buf = binary.LittleEndian.AppendUint16(buf, channels*2)
	buf = binary.LittleEndian.AppendUint16(buf, 16)
	buf = append(buf, "data"...)
	buf = binary.LittleEndian.AppendUint32(buf, dataSize)
	for _, s := range samples {
		buf = binary.LittleEndian.AppendUint16(buf, uint16(s))
	}
	_, err = f.Write(buf)
	return err
}
