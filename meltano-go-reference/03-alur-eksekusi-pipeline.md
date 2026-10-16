> Bagian 3 dari 16 - [Daftar Isi](README.md)

## 3. Alur Eksekusi Pipeline

### 3.1 Jalur modern: `meltano run`

**Tahap 1 - Parsing & perluasan job** (`src/meltano/cli/run.py:193-218`)

`BlockParser.__init__` (`src/meltano/core/block/block_parser.py:79-161`) menerima daftar token. Sebelum
parsing, `_expand_jobs` (`src/meltano/core/block/block_parser.py:168-193`) mengganti nama job/task set
dengan `flat_args`-nya:

> Docstring `src/meltano/core/block/block_parser.py:171-173`:
> `["somejob", "dbt:run"] -> ["tap", "target", "dbt:run"]`

Setiap nama dipecah pada `:` menjadi `(plugin_name, command_name)`, di-resolve lewat
`ProjectPluginsService.find_plugin`, dan nama yang ambigu (job + plugin) ditolak dengan `ClickException`
(`src/meltano/core/block/block_parser.py:142-146`).

**Tahap 2 - Pembentukan BlockSet** (`src/meltano/core/block/block_parser.py:195-332`)

`find_blocks` mengiterasi dan memanggil `_find_next_elb_set`, yang:

- menolak set yang bukan diawali `PluginType.EXTRACTORS` (baris 265-271),
- menambahkan mapper selama tipe plugin ada di `CONSUMERS` (baris 283-316),
- menambahkan loader sebagai tail dan mengembalikan `ExtractLoadBlocks` (baris 318-322),
- melempar `BlockSetValidationError("Loader missing in block set!")` bila tidak ada loader (baris 332).

**Tahap 3 - Validasi**

Dua level: `validate_block_sets` untuk semua set (`src/meltano/core/block/block_parser.py:54-75`), dan
`ExtractLoadBlocks.validate_set` per set (`src/meltano/core/block/extract_load.py:468-487`): head tidak boleh
consumer, tail tidak boleh producer, intermediate wajib producer **dan** consumer. Validasi ketiga terjadi
saat linking: consumer tanpa upstream producer melempar
`BlockSetValidationError("run step requires input but has no upstream")`
(`src/meltano/core/block/extract_load.py:678-681`).

**Tahap 4 - Instalasi plugin** (`src/meltano/cli/run.py:220-224`) -> `PluginInstallService.install_plugin`
(`src/meltano/core/plugin_install_service.py:286`).

**Tahap 5 - Eksekusi blok** (`src/meltano/cli/run.py:264-341`)

`_run_blocks` memanggil `blk.run()` per blok secara serial; kegagalan **satu blok menghentikan seluruh run**
(docstring `src/meltano/cli/run.py:159`: *"A failure in any block will cause the entire run to abort."*),
dibuktikan oleh `ctx.exit(1)` pada `RunnerError` (baris 297-311).

**Tahap 6 - Konteks job & state** (`src/meltano/core/block/extract_load.py:337-381`)

Kunci: **tanpa environment aktif, job = None dan tidak ada state**.

```python
# extract_load.py:356-368
if not self.context.project.environment:
    self.context.job = None
elif self.context.update_state:
    state_id = generate_state_id(project, suffix, self.head, self.tail)
    self.context.job = Job(job_name=state_id)
```

`generate_state_id` (`src/meltano/core/block/extract_load.py:894-931`) menghasilkan
`{environment}:{head}-to-{tail}` dengan delimiter dari `core/constants.py`
(`STATE_ID_COMPONENT_DELIMITER`).

> Catatan verifikasi: signature `generate_state_id(project, state_id_suffix, consumer, producer)`
> (`src/meltano/core/block/extract_load.py:894-899`) menerima `self.head` (extractor) pada parameter bernama
> `consumer`, dan `self.tail` (loader) pada parameter bernama `producer` (baris 360-365). Nama parameter
> tersebut terbalik terhadap maknanya, tetapi output string-nya sesuai dokumentasi
> `src/meltano/cli/run.py:167-172` yaitu `prod:tap-gitlab-to-target-postgres`. Ini jebakan baca kode yang
> perlu diwaspadai.

**Tahap 7 - Job lifecycle** (`src/meltano/core/job/job.py:292-339`)

`Job.run` adalah async context manager: `self.start()` -> `save()` -> `_handling_sigterm` -> `_heartbeating`
-> `yield` -> `success()` -> `save()`. Pada exception, jika job masih RUNNING, `fail(error=...)` lalu
re-raise.

### 3.2 Jalur legacy: `meltano el` / `elt`

(`src/meltano/cli/elt.py:165` `el`, `src/meltano/cli/elt.py:246` `elt`) - keduanya memanggil
`_run_el_command` (`src/meltano/cli/elt.py:302`).

Perbedaan kunci vs `run`:

| Aspek | `el`/`elt` | `run` |
|---|---|---|
| Bangun konteks | `ELTContextBuilder` + `with_*` fluent (`cli/elt.py:429-445`, `elt_context.py:215`) | `ELBContextBuilder` via `BlockParser` |
| State ID | user-supplied atau `%Y-%m-%dT%H%M%S--{extractor}--{loader}` (`cli/elt.py:350-361`) | `{env}:{tap}-to-{target}` |
| Runner | `SingerRunner` (`cli/elt.py:615`, `runner/singer.py:25`) | `ExtractLoadBlocks` + `ELBExecutionManager` |
| Topologi | tap->target saja | N blok, mapper di tengah |
| Platform | ditolak di Windows (`cli/elt.py:324-329`) | lintas platform |
| Status | deprecated (`cli/elt.py:278`) | direkomendasikan |

Fakta penting yang sering salah diasumsikan: **tanpa `--state-id`, state incremental tidak dipakai ulang
antar run** - Meltano hanya memberi peringatan (`src/meltano/cli/elt.py:350-361`).

---

**Navigasi:** [Bagian 2](02-peta-modul-dan-dependensi.md) - [Daftar Isi](README.md) - [Bagian 4](04-connector-dan-plugin.md)
