> Bagian 12 dari 16 - [Daftar Isi](README.md)

## 12. Hal yang Belum Terverifikasi

Supaya tidak melebih-klaim, berikut yang **tidak** dibuktikan dari source code:

1. **Perilaku runtime connector nyata.** Repository ini tidak menyertakan tap/target Singer, sehingga
   interaksi nyata (mis. apakah target benar-benar meng-emit STATE setiap N baris) tidak dapat diverifikasi
   dari sini; hanya kontraknya yang terbukti.
2. **Isi `state.json` per connector.** Skema isi state ditentukan connector, tidak ada di repo ini.
3. **Implementasi backend state S3/GCS/Azure.** Yang dibaca penuh adalah `state_store/base.py`, `db.py`, dan
   bagian `filesystem.py`. `s3/backend.py`, `google/backend.py`, `azure/backend.py`, dan sisa
   `state_store/filesystem.py` (538 baris) belum dibaca seluruhnya.
4. **Semantik penulisan config plugin di luar `shutil.copy`.** Bagaimana file `config` utama benar-benar
   ditulis berada di jalur `SettingsService`/`PluginSettingsService` yang belum ditelusuri habis.
5. **Detail `SettingsService.get_with_metadata`.** Daftar method sudah teridentifikasi
   (`src/meltano/core/settings_service.py:183-660`), tetapi logika presedensi sumber nilai
   (default -> meltano.yml -> env -> db -> config override) belum dibaca seluruhnya.
6. **Meltano Cloud / Airflow / Dagster orchestrator** (`plugin/airflow.py`, `core/cloud/`) tidak dianalisis;
   keberadaannya teridentifikasi tetapi di luar cakupan.
7. **Apakah `meltano state` CLI men-subscribe state store yang sama** dengan jalur pipeline - indikasi kuat
   ya (dari `src/meltano/cli/state.py:105` yang memanggil `.state_service` pada block dari `BlockParser`),
   tetapi alur tersebut tidak ditelusuri penuh.

---

**Navigasi:** [Bagian 11](11-risiko-dan-keputusan.md) - [Daftar Isi](README.md) - [Bagian 13](13-arsitektur-control-plane.md)
