package skills

import (
	"archive/zip"
	"bytes"
	"embed"
	"sync"
)

//go:embed work-schedule-sync/SKILL.md
var Sync string

//go:embed work-schedule-sync/SKILL.md work-schedule-sync/scripts/sync_queue.py
var files embed.FS

var archiveOnce sync.Once
var archiveBytes []byte
var archiveError error

func Archive() ([]byte, error) {
	archiveOnce.Do(func() {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		for _, name := range []string{"work-schedule-sync/SKILL.md", "work-schedule-sync/scripts/sync_queue.py"} {
			data, err := files.ReadFile(name)
			if err != nil {
				archiveError = err
				return
			}
			entry, err := w.Create(name)
			if err != nil {
				archiveError = err
				return
			}
			if _, err = entry.Write(data); err != nil {
				archiveError = err
				return
			}
		}
		archiveError = w.Close()
		archiveBytes = b.Bytes()
	})
	return archiveBytes, archiveError
}
