//go:build windows

package meltano

import "os"

// terminate pada Windows tidak bisa mengirim SIGTERM ke proses lain,
// sehingga langsung memakai Kill. Konsekuensinya proses Meltano tidak sempat
// menjalankan graceful shutdown dan exit code adalah hasil Kill, bukan 143.
// Jalankan worker di Linux bila cancel yang rapi penting.
func terminate(p *os.Process) error {
	if p == nil {
		return nil
	}
	return p.Kill()
}
