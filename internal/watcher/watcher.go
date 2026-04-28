package watcher

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

var defaultIgnore = []string{
	".git", "node_modules", ".next", "dist", "build",
	".upuai", "__pycache__", ".venv", "vendor", "bin",
}

type WatchCallback func() error

func Watch(dir string, debounce time.Duration, callback WatchCallback) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	if err := addDirRecursive(watcher, dir); err != nil {
		return err
	}

	var timer *time.Timer

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if shouldIgnore(event.Name) {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(debounce, func() {
					_ = callback()
				})
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			return err
		}
	}
}

func addDirRecursive(watcher *fsnotify.Watcher, dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if shouldIgnore(path) {
				return filepath.SkipDir
			}
			return watcher.Add(path)
		}
		return nil
	})
}

func shouldIgnore(path string) bool {
	for _, ignore := range defaultIgnore {
		if strings.Contains(path, string(filepath.Separator)+ignore+string(filepath.Separator)) ||
			strings.HasSuffix(path, string(filepath.Separator)+ignore) ||
			filepath.Base(path) == ignore {
			return true
		}
	}
	return false
}
