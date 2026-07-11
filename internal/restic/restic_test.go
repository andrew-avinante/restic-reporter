package restic

import (
	"strings"
	"testing"
)

// sampleJSON is representative of `restic backup --json` output: several status
// progress lines, then the final summary object.
const sampleJSON = `{"message_type":"status","percent_done":0.5,"total_files":10}
{"message_type":"status","percent_done":0.9,"total_files":10}
{"message_type":"summary","files_new":3,"files_changed":2,"files_unmodified":5,"data_added":1048576,"total_files_processed":10,"total_bytes_processed":52428800,"snapshot_id":"abc123","tree_blobs":4}
`

func TestScanSummary(t *testing.T) {
	r := &Runner{}
	s, err := r.scanSummary(strings.NewReader(sampleJSON))
	if err != nil {
		t.Fatalf("scanSummary: %v", err)
	}
	if s == nil {
		t.Fatal("expected a summary, got nil")
	}
	checks := []struct {
		name string
		got  int64
		want int64
	}{
		{"files_new", s.FilesNew, 3},
		{"files_changed", s.FilesChanged, 2},
		{"files_unmodified", s.FilesUnmodified, 5},
		{"data_added", s.DataAdded, 1048576},
		{"total_files_processed", s.TotalFilesProcessed, 10},
		{"total_bytes_processed", s.TotalBytesProcessed, 52428800},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
	if s.SnapshotID != "abc123" {
		t.Errorf("snapshot_id = %q, want %q", s.SnapshotID, "abc123")
	}
}

// TestScanSummaryNoSummary confirms we tolerate output without a summary line
// (e.g. a run that errored mid-stream) rather than crashing.
func TestScanSummaryNoSummary(t *testing.T) {
	r := &Runner{}
	s, err := r.scanSummary(strings.NewReader(`{"message_type":"status","percent_done":0.1}` + "\n"))
	if err != nil {
		t.Fatalf("scanSummary: %v", err)
	}
	if s != nil {
		t.Errorf("expected nil summary, got %+v", s)
	}
}
