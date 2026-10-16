> Bagian 1 dari 16 - [Daftar Isi](README.md)

## 1. Ringkasan Eksekutif

Meltano adalah **orkestrator proses eksternal berbasis konvensi Singer**, bukan engine ETL. Ia tidak
memindahkan data; ia menyiapkan konfigurasi, menjalankan subprocess connector yang dihubungkan lewat pipe
stdin/stdout, lalu menyimpan bookmark state di database sistem.

Temuan arsitektural paling penting:

1. **Dua jalur eksekusi paralel.** Jalur legacy `meltano el`/`meltano elt` memakai `SingerRunner` dua proses
   langsung. Jalur modern `meltano run` memakai abstraksi `BlockSet`/`IOBlock`/`SingerBlock` yang mendukung
   rantai N proses (extractor -> mapper -> loader) dan hanya berjalan penuh bila ada environment aktif.

2. **Batas engine-connector adalah filesystem + pipe, bukan API.** Meltano menulis file JSON
   (`tap.<uuid>.config.json`, `state.json`, `tap.properties.json`) ke run dir, lalu mengeksekusi connector
   dengan argv + environment. Data mengalir sebagai JSON line-delimited di stdout->stdin. Meltano
   **tidak punya visibilitas ke dalam batch/transaksi data** - hanya ke baris STATE.

3. **Checkpoint = setiap pesan STATE dari target.** `BookmarkWriter.writeline` dijalankan sebagai output
   handler pada stdout target; setiap STATE langsung dipersist ke state store. Granularitas checkpoint
   sepenuhnya ditentukan oleh connector, bukan oleh Meltano.

4. **Tidak ada retry untuk eksekusi plugin.** Retry hanya ada di tiga tempat: koneksi database
   (`database_max_retries`/`database_retry_timeout`), request telemetry/hub, dan perolehan lock state store.
   Kegagalan tap/target tidak pernah diulang otomatis.

5. **Konsistensi bersifat at-least-once.** Penulisan ke destination dan persistensi state tidak berada dalam
   satu transaksi (bahkan tidak dalam satu proses), sehingga crash di antara keduanya menyebabkan baris
   ter-replikasi ulang pada run berikutnya. Meltano tidak melakukan deduplikasi.

6. **Reliabilitas proses bergantung pada heartbeat + stale-job failer.** Job yang mati tanpa membersihkan
   statusnya akan ditandai FAIL setelah 5 menit tanpa heartbeat (atau 24 jam untuk job legacy tanpa
   heartbeat).

---

**Navigasi:** [Daftar Isi](README.md) - [Bagian 2: Peta Modul](02-peta-modul-dan-dependensi.md)
