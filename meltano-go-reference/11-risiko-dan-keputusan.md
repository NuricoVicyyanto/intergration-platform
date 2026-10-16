> Bagian 11 dari 16 - [Daftar Isi](README.md)

## 11. Risiko Teknis dan Keputusan Arsitektur yang Perlu Ditetapkan

### 11.1 Risiko yang berasal dari kelemahan Meltano (jangan direplikasi)

| Risiko | Bukti di Meltano | Mitigasi yang disarankan |
|---|---|---|
| Duplikasi data pada kegagalan | State dipersist terpisah dari penulisan data (`target.py:66-121`) | Commit state di transaksi destination yang sama; idempotent upsert; LSN sebagai bagian state |
| Tidak ada retry untuk kegagalan transien | Tidak ada retry plugin (bagian 6.5) | Klasifikasi error + backoff eksponensial + batas percobaan |
| Advisory check, bukan lock | `JobFinder.latest_running` (`extract_load.py:530`) | Queue dengan klaim atomik + lease dengan `expires_at` |
| Lock state naif | Timestamp file lock (`filesystem.py:289-295`) | Conditional write / compare-and-swap dengan `version` |
| Batas ukuran pesan baris | `elt.buffer_size // 2` (`singer.py:320`) | Batch biner in-process; streaming decoder, bukan line reader |
| Exit code producer di-mask saat consumer gagal lebih dulu | `_producer_code = 0` (`extract_load.py:725-733`) | Simpan seluruh error context, jangan menormalkan exit code |
| Config/state run dir dihapus setelah run | TODO di `block/singer.py:235-237` | Simpan artefak run gagal untuk forensik, dengan TTL |
| State tidak berversi/skema | Hanya cek key `singer_state` (`state_service.py:149`) | State bertipe + `schema_version` + migrasi state |

### 11.2 Keputusan yang harus ditetapkan lebih dulu

1. **Semantik konsistensi.** exactly-once (butuh idempotent sink + state dalam transaksi) vs at-least-once
   (lebih sederhana, butuh toleransi duplikasi di sisi konsumen). Ini menentukan desain `Sink`.
2. **Mekanisme incremental per database.** Watermark berbasis kolom (sederhana, tidak menangkap `DELETE`,
   tidak menangkap update pada kolom watermark) vs CDC/log-based (menangkap semua, jauh lebih kompleks,
   butuh replikasi log aktif). Untuk Oracle dan SQL Server ini keputusan yang sangat mahal bila salah.
3. **Model tipe data.** Satu model tipe internal netral + mapping per dialect, atau passthrough. Rekomendasi:
   model internal netral dengan `TypeMapping` per dialect, diuji dengan property-based test.
4. **Isolasi konektor.** In-process interface (cepat, mudah di-debug) vs subprocess/WASM (isolasi, bahasa
   bebas). Untuk Go, rekomendasi: in-process untuk konektor inti, subprocess hanya untuk tool eksternal yang
   tidak bisa dihindari (`pg_dump`, LogMiner).
5. **Model state.** State per (job, stream/table) bukan per job, agar tabel yang gagal bisa di-retry tanpa
   mengulang seluruh stream. Meltano memakai per-state-id (`src/meltano/core/job_state.py:37`) - granularitas
   ini terlalu kasar.
6. **Format checkpoint.** JSON bebas (kompatibel Singer, mudah integrasi) vs protobuf/typed struct (aman,
   cepat, butuh versioning). Rekomendasi: typed struct dengan varian payload per konektor.
7. **Batas resource.** Apakah worker membatasi memori, atau apakah engine memaksa backpressure berdasarkan
   estimasi ukuran batch. Melimpahkan ke operator adalah sumber penderitaan.
8. **Schema evolution.** Apakah DDL pada sumber otomatis diterapkan ke destination (risiko kehilangan data)
   atau hanya dilaporkan (aman tapi butuh intervensi).

### 11.3 Beban implementasi per database (estimasi relatif)

Postgres paling murah (logical replication matang, `pgoutput` mudah dipakai dari Go). MySQL menengah
(`go-mysql`/binlog). SQL Server menengah-ke-mahal (change tracking vs CDC, driver `mssql`). Oracle paling
mahal (LogMiner, driver non-bebas-royalti `godror`, kompleksitas tipe `NUMBER`/`DATE`/`TIMESTAMP WITH TIME
ZONE`). Urutan bertahap di bagian 10 sudah mengasumsikan ini.

---

**Navigasi:** [Bagian 10](10-roadmap-implementasi.md) - [Daftar Isi](README.md) - [Bagian 12](12-belum-terverifikasi.md)
