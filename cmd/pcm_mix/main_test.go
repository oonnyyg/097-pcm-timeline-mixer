package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"example.com/pcm-timeline-mixer/internal/mixer"
)

func TestRunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	src := &mixer.Wave{SampleRate: 8000, Channels: 1, Samples: []int16{100, 200, 300}}
	if err := mixer.WriteWave(filepath.Join(dir, "a.wav"), src); err != nil {
		t.Fatal(err)
	}
	task := `{"sources":[{"id":"a","path":"a.wav"}],
		"clips":[{"id":"c","source":"a","src_start":0,"src_end":3,"out_start":1,"gain":2.5}]}`
	taskPath := filepath.Join(dir, "task.json")
	if err := os.WriteFile(taskPath, []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := run([]string{taskPath, "--out", outDir}); err != nil {
		t.Fatal(err)
	}
	mix, err := mixer.ReadWave(filepath.Join(outDir, "mix.wav"))
	if err != nil {
		t.Fatal(err)
	}
	want := []int16{0, 250, 500, 750}
	if mix.Frames() != 4 {
		t.Fatalf("frames %d", mix.Frames())
	}
	for i, s := range mix.Samples {
		if s != want[i] {
			t.Fatalf("sample %d: got %d want %d", i, s, want[i])
		}
	}
	repRaw, err := os.ReadFile(filepath.Join(outDir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rep mixer.Report
	if err := json.Unmarshal(repRaw, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Frames != 4 || rep.PerChannel[0].PeakAbs != 750 {
		t.Fatalf("report %+v", rep)
	}
}

func TestRunRefusesToOverwriteSource(t *testing.T) {
	dir := t.TempDir()
	src := &mixer.Wave{SampleRate: 8000, Channels: 1, Samples: []int16{1}}
	if err := mixer.WriteWave(filepath.Join(dir, "mix.wav"), src); err != nil {
		t.Fatal(err)
	}
	task := `{"sources":[{"id":"a","path":"mix.wav"}],
		"clips":[{"id":"c","source":"a","src_start":0,"src_end":1,"out_start":0,"gain":1}]}`
	taskPath := filepath.Join(dir, "task.json")
	if err := os.WriteFile(taskPath, []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{taskPath, "--out", dir}); err == nil {
		t.Fatal("expected error when output would overwrite source")
	}
}

func TestRunInvalidTaskProducesNoOutput(t *testing.T) {
	dir := t.TempDir()
	src := &mixer.Wave{SampleRate: 8000, Channels: 1, Samples: []int16{1}}
	if err := mixer.WriteWave(filepath.Join(dir, "a.wav"), src); err != nil {
		t.Fatal(err)
	}
	task := `{"sources":[{"id":"a","path":"a.wav"}],"clips":[]}`
	taskPath := filepath.Join(dir, "task.json")
	if err := os.WriteFile(taskPath, []byte(task), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "out")
	if err := run([]string{taskPath, "--out", outDir}); err == nil {
		t.Fatal("expected error for empty clip list")
	}
	if _, err := os.Stat(filepath.Join(outDir, "mix.wav")); !os.IsNotExist(err) {
		t.Fatal("mix.wav must not exist after failure")
	}
	if _, err := os.Stat(filepath.Join(outDir, "report.json")); !os.IsNotExist(err) {
		t.Fatal("report.json must not exist after failure")
	}
}
