# Analisis Arsitektur Meltano — Titik Masuk

> **Artefak analisis lokal.** File ini **bukan** bagian dari proyek Meltano upstream. Isinya hanya
> penunjuk ke dokumen analisis yang berada di `meltano-go-reference/`. `README.md` resmi Meltano tidak
> diubah. Jika tidak diperlukan lagi, file ini dan folder `meltano-go-reference/` dapat dihapus tanpa
> memengaruhi apa pun di repository Meltano.

**Basis analisis:** source code Meltano **v4.4.1**, commit `74725f257`, dibaca langsung dari repository ini.

**Tujuan:** memahami arsitektur internal dan alur eksekusi ELT Meltano sebagai referensi perancangan sistem
replikasi database berbasis Golang.

**Dokumen lengkap:** [meltano-go-reference/README.md](meltano-go-reference/README.md)

## Daftar Bagian

| # | Bagian | File |
|---|---|---|
| 1 | Ringkasan Eksekutif | [01-ringkasan-eksekutif.md](meltano-go-reference/01-ringkasan-eksekutif.md) |
| 2 | Peta Modul dan Dependensi | [02-peta-modul-dan-dependensi.md](meltano-go-reference/02-peta-modul-dan-dependensi.md) |
| 3 | Alur Eksekusi Pipeline | [03-alur-eksekusi-pipeline.md](meltano-go-reference/03-alur-eksekusi-pipeline.md) |
| 4 | Mekanisme Connector dan Plugin | [04-connector-dan-plugin.md](meltano-go-reference/04-connector-dan-plugin.md) |
| 5 | State, Checkpoint, dan Recovery | [05-state-checkpoint-recovery.md](meltano-go-reference/05-state-checkpoint-recovery.md) |
| 6 | Subprocess dan Reliability | [06-subprocess-dan-reliability.md](meltano-go-reference/06-subprocess-dan-reliability.md) |
| 7 | Diagram Arsitektur dan Sequence | [07-diagram-arsitektur.md](meltano-go-reference/07-diagram-arsitektur.md) |
| 8 | Pemetaan Konsep Meltano ke Arsitektur Golang | [08-pemetaan-ke-golang.md](meltano-go-reference/08-pemetaan-ke-golang.md) |
| 9 | Struktur Direktori Proyek Go yang Direkomendasikan | [09-struktur-direktori-go.md](meltano-go-reference/09-struktur-direktori-go.md) |
| 10 | Roadmap Implementasi Bertahap | [10-roadmap-implementasi.md](meltano-go-reference/10-roadmap-implementasi.md) |
| 11 | Risiko Teknis dan Keputusan Arsitektur | [11-risiko-dan-keputusan.md](meltano-go-reference/11-risiko-dan-keputusan.md) |
| 12 | Hal yang Belum Terverifikasi | [12-belum-terverifikasi.md](meltano-go-reference/12-belum-terverifikasi.md) |
| 13 | Arsitektur Control Plane & Kontrak Integrasi Meltano | [13-arsitektur-control-plane.md](meltano-go-reference/13-arsitektur-control-plane.md) |
| 14 | Referensi API Control Plane | [14-api-reference.md](meltano-go-reference/14-api-reference.md) |
| 15 | Menjalankan Meltano dan Menyambungkannya ke Control Plane | [15-menjalankan-meltano.md](meltano-go-reference/15-menjalankan-meltano.md) |
| 16 | Cara Menjalankan Meltano + Control Plane Golang (runbook) | [16-cara-menjalankan-meltano-golang.md](meltano-go-reference/16-cara-menjalankan-meltano-golang.md) |

## Cara Membaca

- **Bagian 1–7** adalah **temuan terbukti dari source code**. Setiap klaim disertai path file dan nama
  simbol/kelas/fungsi, misalnya `src/meltano/core/block/extract_load.py:638` (`_link_io`).
- **Bagian 8–15** adalah **rekomendasi desain dan panduan** untuk proyek Golang, bukan deskripsi perilaku
  Meltano. Bagian 13 memuat arsitektur control plane beserta kontrak integrasinya (argv, env var, exit code,
  cara membaca status dan state); bagian 14 memuat referensi API lengkap dengan spesifikasi OpenAPI;
  bagian 15 memuat panduan operasional menjalankan Meltano dan menyambungkannya ke control plane.
- **Bagian 12** mencatat klaim yang belum bisa dibuktikan dari source code, supaya tidak terjadi
  over-claiming; bagian 13.14 memuat hal yang perlu diverifikasi saat implementasi.

Semua path di dalam dokumen relatif terhadap root repository Meltano.

## Implementasi

Kerangka control plane Golang hasil rancangan bagian 13–15 ada di
[`controlplane/README.md`](controlplane/README.md) — monorepo dengan dua binary
(`cmd/control-plane`, `cmd/worker`), tanpa dependency eksternal, sudah lolos
`go build`, `go vet`, dan `go test`.

- **Runbook end-to-end:** [bagian 16](meltano-go-reference/16-cara-menjalankan-meltano-golang.md)
- **Postman collection siap impor:** [control-plane.postman_collection.json](controlplane/postman/control-plane.postman_collection.json)
- **Spesifikasi OpenAPI 3.1:** [14-openapi.yaml](meltano-go-reference/14-openapi.yaml)

## Temuan Kunci dalam Satu Paragraf

Meltano adalah orkestrator tipis di atas konvensi Singer: ia menyelesaikan definisi plugin berlapis
(`meltano.yml` -> lockfile -> Hub), menulis konfigurasi ke disk, menjalankan tap dan target sebagai
subprocess yang dihubungkan pipe stdin/stdout, menyimpan setiap pesan STATE target sebagai checkpoint lewat
`StateStoreManager`, dan mengawasi kesehatan proses dengan heartbeat plus stale-job failer. Yang perlu
diwaspadai saat mengadaptasinya: **tidak ada retry** untuk eksekusi plugin, checkpoint **tidak transaksional**
dengan penulisan data (konsekuensinya at-least-once dan berpotensi duplikasi), pengamanan run ganda hanya
berupa query database, locking state naif, serta granularitas state per-state-ID alih-alih per-tabel.
