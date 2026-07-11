package store

import (
	"os"
	"time"

	"shepherd/internal/todo"
)

// AppendArchive appends done items to a sibling archive.md (created if absent).
func AppendArchive(todoFile string, items []todo.Item) error {
	f, err := os.OpenFile(ArchivePath(todoFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(Serialize(items))
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
