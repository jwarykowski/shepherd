package store

import (
	"os"
	"shepherd/internal/todo"
	"time"
)

// AppendArchive appends done items to a sibling archive.md (created if absent),
// starting on a fresh line if a hand edit left the last one unterminated.
func AppendArchive(todoFile string, items []todo.Item) error {
	p := ArchivePath(todoFile)
	text := Serialize(items)
	if data, err := os.ReadFile(p); err == nil && len(data) > 0 && data[len(data)-1] != '\n' {
		text = "\n" + text
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(text)
	if werr == nil {
		werr = f.Sync()
	}
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

// FileModTime returns the file's mtime, or the zero time if it can't be stat'd.
func FileModTime(p string) time.Time {
	fi, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}
