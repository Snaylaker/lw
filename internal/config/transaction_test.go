package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestConfigUpdatesSerializeTheWholeReadModifyWriteTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := updateConfig(path, func(current *StoredConfig) (struct{}, bool, error) {
			close(entered)
			<-release
			current.WorktreeRoot = "~/worktrees"
			return struct{}{}, true, nil
		})
		firstDone <- err
	}()
	<-entered

	secondDone := make(chan error, 1)
	go func() {
		_, err := SetPruneMerged(true, path)
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second update escaped the first transaction lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	stored, err := ReadStoredConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.WorktreeRoot != "~/worktrees" || !stored.PruneMerged {
		t.Fatalf("concurrent updates were not both preserved: %+v", stored)
	}
}
