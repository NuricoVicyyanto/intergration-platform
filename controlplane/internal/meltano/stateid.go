package meltano

import (
	"fmt"
	"strings"
)

// StateIDDelimiter adalah pemisah komponen state_id Meltano
// (src/meltano/core/constants.py: STATE_ID_COMPONENT_DELIMITER = ":").
const StateIDDelimiter = ":"

// StateID menghitung state_id dengan formula identik dengan
// generate_state_id (src/meltano/core/block/extract_load.py:894-931):
//
//	{environment}:{extractor}-to-{loader}[:{suffix}]
//
// `meltano run` TIDAK menerima --state-id; nilainya dibentuk dari komponen ini.
// Control plane harus menghitungnya sendiri karena string yang sama dipakai untuk
// `meltano state get`, mencocokkan kolom runs.job_name, dan menurunkan path log.
//
// Komponen kosong dilewati, dan setiap komponen tidak boleh mengandung ":" -
// Meltano menolaknya dengan RunnerError (extract_load.py:925-929).
func StateID(environment, extractor, loader, suffix string) (string, error) {
	parts := make([]string, 0, 3)

	// Slot pertama: environment. Kosong berarti dilewati.
	if environment != "" {
		parts = append(parts, environment)
	}
	// Slot kedua selalu ada: "{extractor}-to-{loader}".
	parts = append(parts, extractor+"-to-"+loader)
	// Slot ketiga opsional.
	if suffix != "" {
		parts = append(parts, suffix)
	}

	for _, p := range parts {
		if strings.Contains(p, StateIDDelimiter) {
			return "", fmt.Errorf(
				"komponen state_id tidak boleh mengandung %q: %q", StateIDDelimiter, p)
		}
	}
	return strings.Join(parts, StateIDDelimiter), nil
}

// reservedWindowsNames mengikuti _reserved_windows_filenames di
// src/meltano/core/utils/__init__.py.
var reservedWindowsNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// SanitizeFilename meniru sanitize_filename
// (src/meltano/core/utils/__init__.py:1034-1075) yang dipakai Meltano saat
// membentuk direktori log dari state_id
// (src/meltano/core/project_dirs_service.py:167-188).
//
// Urutan transformasi yang direplikasi:
//  1. hanya ASCII (karakter non-ASCII dibuang)
//  2. pemisah path menjadi spasi
//  3. setiap rentetan whitespace menjadi satu "_"
//  4. hanya [A-Za-z0-9_.-] yang dipertahankan
//  5. karakter "." dan "_" di ujung dipangkas
//  6. nama yang bentrok dengan nama reserved Windows diberi awalan "_"
//
// Perbedaan yang disengaja dari implementasi Python: Meltano menjalankan
// normalisasi NFKD sebelum membuang non-ASCII, sedangkan versi Go ini tidak
// (menghindari dependency golang.org/x/text). Akibatnya karakter beraksen
// seperti "é" dibuang seluruhnya, bukan disederhanakan menjadi "e". Untuk
// state_id yang normal - yang hanya berisi huruf, angka, "-", "_", dan "." -
// hasil kedua implementasi identik.
//
// Verifikasi: nilai yang benar adalah yang dikembalikan `meltano logs dir`.
// Jangan jadikan fungsi ini satu-satunya sumber kebenaran untuk path log.
func SanitizeFilename(name string) string {
	// 1. buang non-ASCII, dan sekaligus 2. ubah pemisah path menjadi spasi.
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r == '/' || r == '\\':
			b.WriteByte(' ')
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\v' || r == '\f':
			// Semua whitespace ASCII diperlakukan sama, karena langkah berikutnya
			// memakai strings.Fields yang juga memperlakukannya sebagai pemisah.
			b.WriteByte(' ')
		case r >= 0x20 && r < 0x7f:
			b.WriteRune(r)
		default:
			// dibuang
		}
	}
	ascii := b.String()

	// 3. rentetan whitespace -> "_"
	ascii = strings.Join(strings.Fields(ascii), "_")

	// 4. hanya [A-Za-z0-9_.-]
	var c strings.Builder
	c.Grow(len(ascii))
	for _, r := range ascii {
		switch {
		case r >= 'A' && r <= 'Z',
			r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '_', r == '-', r == '.':
			c.WriteRune(r)
		}
	}
	out := c.String()

	// 5. pangkas "." dan "_" di kedua ujung
	out = strings.Trim(out, "._")

	// 6. hindari nama reserved Windows
	if out != "" {
		head := out
		if i := strings.IndexByte(out, '.'); i >= 0 {
			head = out[:i]
		}
		if _, bad := reservedWindowsNames[strings.ToUpper(head)]; bad {
			out = "_" + out
		}
	}
	return out
}
