# Reverse Engineering Meltano untuk Referensi Sistem Replikasi Database Golang

Dokumen ini adalah hasil pembacaan langsung terhadap source code Meltano **v4.4.1** pada commit `74725f257`.
Tujuannya: memahami arsitektur internal dan alur eksekusi ELT Meltano, lalu memakainya sebagai referensi
untuk merancang sistem replikasi database berbasis Golang.

**Konvensi path.** Semua path file di dokumen ini relatif terhadap root repository Meltano, misalnya
`src/meltano/cli/run.py:137` berarti file tersebut baris 137.

**Cara membaca dokumen ini.** Bagian 1-7 adalah **temuan terbukti dari source code** (setiap klaim disertai
path dan simbol). Bagian 8-16 adalah **rekomendasi desain dan panduan** untuk proyek Golang, bukan deskripsi
perilaku Meltano - bagian 13 arsitektur control plane, bagian 14 referensi API, bagian 15 panduan
menjalankan Meltano, dan bagian 16 runbook end-to-end. Bagian 12 mencatat hal yang belum terverifikasi;
bagian 13.14, 14.11, dan 15.11 memuat hal yang perlu diverifikasi saat implementasi.

**Berkas pendukung:**

| Berkas | Isi |
|---|---|
| [`14-openapi.yaml`](14-openapi.yaml) | spesifikasi OpenAPI 3.1 dari API control plane |
| [`../controlplane/postman/control-plane.postman_collection.json`](../controlplane/postman/control-plane.postman_collection.json) | Postman collection siap impor, 20 request |

## Catatan Metode Tooling (REA)

Sesi ini menyediakan REA MCP (~40 tool untuk native binary, managed/.NET, Mach-O, APK/Dart AOT, WASM/EVM,
JavaScript bundle/ASAR, Electron, packet capture, SQLite snapshot, dan source map). Pemeriksaan
`get_evidence_bundle(detail="summary")` mengembalikan `total_retained_records: 0`, dan tidak ada satupun
tool REA yang menganalisis tree source Python.

Skill `reverse-engineer-anything` menyatakan eksplisit: *"For ordinary analysis of a complete source
repository, use normal repository tools and do not run REA readiness or provider commands."* Repository ini
murni Python source tanpa artifak terkompilasi yang di-vendor, sehingga analisis dijalankan dengan pembacaan
source aktual.

---

## Daftar Isi

Isi dokumen dipecah per bagian supaya lebih mudah dinavigasi. Setiap file memuat isi bagian aslinya secara utuh, tanpa ada catatan yang dihapus atau diringkas.

| # | Bagian | File |
|---|---|---|
| 1 | Ringkasan Eksekutif | [01-ringkasan-eksekutif.md](01-ringkasan-eksekutif.md) |
| 2 | Peta Modul dan Dependensi | [02-peta-modul-dan-dependensi.md](02-peta-modul-dan-dependensi.md) |
| 3 | Alur Eksekusi Pipeline | [03-alur-eksekusi-pipeline.md](03-alur-eksekusi-pipeline.md) |
| 4 | Mekanisme Connector dan Plugin | [04-connector-dan-plugin.md](04-connector-dan-plugin.md) |
| 5 | State, Checkpoint, dan Recovery | [05-state-checkpoint-recovery.md](05-state-checkpoint-recovery.md) |
| 6 | Subprocess dan Reliability | [06-subprocess-dan-reliability.md](06-subprocess-dan-reliability.md) |
| 7 | Diagram Arsitektur dan Sequence | [07-diagram-arsitektur.md](07-diagram-arsitektur.md) |
| 8 | Pemetaan Konsep Meltano ke Arsitektur Golang | [08-pemetaan-ke-golang.md](08-pemetaan-ke-golang.md) |
| 9 | Struktur Direktori Proyek Go yang Direkomendasikan | [09-struktur-direktori-go.md](09-struktur-direktori-go.md) |
| 10 | Roadmap Implementasi Bertahap | [10-roadmap-implementasi.md](10-roadmap-implementasi.md) |
| 11 | Risiko Teknis dan Keputusan Arsitektur | [11-risiko-dan-keputusan.md](11-risiko-dan-keputusan.md) |
| 12 | Hal yang Belum Terverifikasi | [12-belum-terverifikasi.md](12-belum-terverifikasi.md) |
| 13 | Arsitektur Control Plane & Kontrak Integrasi Meltano | [13-arsitektur-control-plane.md](13-arsitektur-control-plane.md) |
| 14 | Referensi API Control Plane | [14-api-reference.md](14-api-reference.md) |
| 15 | Menjalankan Meltano dan Menyambungkannya ke Control Plane | [15-menjalankan-meltano.md](15-menjalankan-meltano.md) |
| 16 | Cara Menjalankan Meltano + Control Plane Golang (runbook) | [16-cara-menjalankan-meltano-golang.md](16-cara-menjalankan-meltano-golang.md) |

---

## Penutup

Meltano adalah orkestrator tipis di atas Singer: ia menyelesaikan definisi plugin berlapis
(`meltano.yml` -> lockfile -> Hub), menulis konfigurasi ke disk, menjalankan tap dan target sebagai subprocess
yang dihubungkan oleh pipe stdin/stdout, mem-persist setiap pesan STATE target sebagai checkpoint melalui
`StateStoreManager`, dan mengawasi health proses dengan heartbeat plus stale-job failer.

Kekuatannya adalah ekosistem dan pemisahan `partial_state`/`completed_state`. Kelemahan fundamentalnya, yang
semuanya terverifikasi di kode, adalah: tidak ada retry untuk eksekusi plugin, checkpoint tidak transaksional
dengan penulisan data sehingga replikasi bersifat at-least-once, pengamanan run ganda hanya berupa query
database, locking state naif, dan granularitas state per-state-ID (bukan per-tabel).

Untuk sistem replikasi Golang, yang layak diadaptasi adalah model state dua-lapis, state ID deterministik,
heartbeat/stale detection, registry backend yang pluggable, dan pelaporan exit code per peran - sedangkan yang
harus dirancang ulang adalah transport data (in-process channel, bukan JSON line pipe), penjaminan konsistensi
(state commit dalam transaksi destination), retry berklasifikasi, locking dengan optimistic concurrency, dan
queue dengan klaim atomik.

---

*Dokumen ini dihasilkan dari pembacaan source code Meltano pada commit `74725f257` (versi 4.4.1). Tidak ada
file di repository Meltano yang diubah selain penambahan dokumen ini.*
