package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionCacheRoundTripAndOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "last-session.json")
	first := newWorkspace([]string{"left one", "left two", ""}, []string{"right one"})
	if err := saveSessionCache(path, first); err != nil {
		t.Fatal(err)
	}

	loaded, found, err := loadSessionCache(path)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("saved session cache was not found")
	}
	if !equalLines(loaded.Left, first.lines(leftSide)) || !equalLines(loaded.Right, first.lines(rightSide)) {
		t.Fatalf("loaded session = left %#v right %#v", loaded.Left, loaded.Right)
	}

	second := newWorkspace([]string{"new left"}, []string{"new right", "last"})
	if err := saveSessionCache(path, second); err != nil {
		t.Fatal(err)
	}
	loaded, found, err = loadSessionCache(path)
	if err != nil || !found {
		t.Fatalf("reloaded session found=%v err=%v", found, err)
	}
	if !equalLines(loaded.Left, second.lines(leftSide)) || !equalLines(loaded.Right, second.lines(rightSide)) {
		t.Fatalf("overwritten session = left %#v right %#v", loaded.Left, loaded.Right)
	}
}

func TestCachedSessionRestoresOnlyWithoutFileArguments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-session.json")
	cached := newWorkspace([]string{"cached left"}, []string{"cached right"})
	if err := saveSessionCache(path, cached); err != nil {
		t.Fatal(err)
	}

	left, right, restored, err := restoreCachedSession([]string{""}, []string{""}, 0, path)
	if err != nil {
		t.Fatal(err)
	}
	if !restored || !equalLines(left, []string{"cached left"}) || !equalLines(right, []string{"cached right"}) {
		t.Fatalf("cache restore = restored %v left %#v right %#v", restored, left, right)
	}

	left, right, restored, err = restoreCachedSession([]string{"file left"}, []string{""}, 1, path)
	if err != nil {
		t.Fatal(err)
	}
	if restored || !equalLines(left, []string{"file left"}) || !equalLines(right, []string{""}) {
		t.Fatalf("explicit file precedence = restored %v left %#v right %#v", restored, left, right)
	}
}

func TestCorruptSessionCacheIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "last-session.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := loadSessionCache(path); err == nil || found {
		t.Fatalf("corrupt cache found=%v err=%v", found, err)
	}
}
