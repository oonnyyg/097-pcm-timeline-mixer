package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"example.com/pcm-timeline-mixer/internal/mixer"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pcm_mix:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var taskPath, outDir string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out":
			i++
			if i >= len(args) {
				return errors.New("--out requires a directory argument")
			}
			outDir = args[i]
		default:
			if taskPath != "" {
				return fmt.Errorf("unexpected argument %q", args[i])
			}
			taskPath = args[i]
		}
	}
	if taskPath == "" || outDir == "" {
		return errors.New("usage: go run ./cmd/pcm_mix 输入.json --out 输出目录")
	}

	raw, err := os.ReadFile(taskPath)
	if err != nil {
		return err
	}
	var task mixer.Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return fmt.Errorf("invalid task JSON: %w", err)
	}

	// Source paths resolve relative to the task file directory.
	base := filepath.Dir(taskPath)
	waves := map[string]*mixer.Wave{}
	var sampleRate, channels int
	for _, s := range task.Sources {
		if _, dup := waves[s.ID]; dup {
			continue // duplicate IDs are rejected by ValidateTask
		}
		p := s.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		w, err := mixer.ReadWave(p)
		if err != nil {
			return fmt.Errorf("source %q: %w", s.ID, err)
		}
		if len(waves) == 0 {
			sampleRate, channels = w.SampleRate, w.Channels
		} else if w.SampleRate != sampleRate || w.Channels != channels {
			return fmt.Errorf("source %q: sample rate/channels differ from other sources", s.ID)
		}
		waves[s.ID] = w
	}

	if err := mixer.ValidateTask(&task, waves); err != nil {
		return err
	}

	mixPath := filepath.Join(outDir, "mix.wav")
	reportPath := filepath.Join(outDir, "report.json")
	// Outputs must never overwrite a source file.
	for _, s := range task.Sources {
		p := s.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		absSrc, _ := filepath.Abs(p)
		for _, out := range []string{mixPath, reportPath} {
			absOut, _ := filepath.Abs(out)
			if absSrc == absOut {
				return fmt.Errorf("output %s would overwrite source %q", out, s.ID)
			}
		}
	}

	mix, report, err := mixer.Mix(&task, waves, sampleRate, channels)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := mixer.WriteWave(mixPath, mix); err != nil {
		return err
	}
	repJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(reportPath, repJSON, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s and %s (%d frames, %d ch, %d Hz)\n",
		mixPath, reportPath, report.Frames, channels, sampleRate)
	return nil
}
