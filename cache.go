package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const sessionCacheVersion = 1

type cachedSession struct {
	Version int      `json:"version"`
	Left    []string `json:"left"`
	Right   []string `json:"right"`
}

func sessionCachePath() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("find user cache directory: %w", err)
	}
	return filepath.Join(root, "tcomp", "last-session.json"), nil
}

func loadSessionCache(path string) (cachedSession, bool, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cachedSession{}, false, nil
	}
	if err != nil {
		return cachedSession{}, false, fmt.Errorf("read cached session: %w", err)
	}

	var session cachedSession
	if err := json.Unmarshal(content, &session); err != nil {
		return cachedSession{}, false, fmt.Errorf("decode cached session: %w", err)
	}
	if session.Version != sessionCacheVersion {
		return cachedSession{}, false, fmt.Errorf("unsupported cached session version %d", session.Version)
	}
	session.Left = normalizeLines(session.Left)
	session.Right = normalizeLines(session.Right)
	return session, true, nil
}

func saveSessionCache(path string, current workspace) error {
	session := cachedSession{
		Version: sessionCacheVersion,
		Left:    append([]string(nil), current.lines(leftSide)...),
		Right:   append([]string(nil), current.lines(rightSide)...),
	}
	content, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("encode cached session: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".last-session-*")
	if err != nil {
		return fmt.Errorf("create cached session: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure cached session: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write cached session: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync cached session: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close cached session: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("replace cached session: %w", err)
		}
		if retryErr := os.Rename(temporaryPath, path); retryErr != nil {
			return fmt.Errorf("replace cached session: %w", retryErr)
		}
	}
	return nil
}

func restoreCachedSession(left, right []string, fileCount int, path string) ([]string, []string, bool, error) {
	if fileCount > 0 || path == "" {
		return left, right, false, nil
	}
	session, found, err := loadSessionCache(path)
	if err != nil || !found {
		return left, right, false, err
	}
	return session.Left, session.Right, true, nil
}
