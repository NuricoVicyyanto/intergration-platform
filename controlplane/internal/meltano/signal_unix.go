//go:build !windows

package meltano

import (
	"os"
	"syscall"
)

// terminate mengirim SIGTERM agar Meltano menjalankan graceful shutdown
// (handler di src/meltano/core/job/job.py:448-496) dan keluar dengan exit 143.
func terminate(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Signal(syscall.SIGTERM)
}
