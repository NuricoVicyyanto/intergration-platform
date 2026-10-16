> Bagian 5 dari 16 - [Daftar Isi](README.md)

## 5. State, Checkpoint, dan Recovery

### 5.1 Model data state

Tiga konsep berbeda yang mudah tertukar - dan Meltano mendokumentasikannya sendiri di
`src/meltano/core/state_service.py:1-6`:

> *"'State' in this module refers to _Singer_ state, which is held in a Job's 'payload' field. This is not
> to be confused with the Job's 'state' field, which refers to a given job run's status, e.g. 'RUNNING' or
> 'FAILED'."*

| Konsep | Kelas/tabel | Isi |
|---|---|---|
| Status run | `Job` / tabel `runs`, kolom `state` | IDLE/RUNNING/SUCCESS/FAIL/DEAD/STATE_EDIT (`job/job.py:61-69`) |
| Singer bookmark | `Job.payload["singer_state"]` | JSON state terakhir |
| State saat ini | `JobState` / tabel `state` | `state_id`, `partial_state`, `completed_state` (`job_state.py:27-42`) |

Kunci state: `SINGER_STATE_KEY = "singer_state"` (`src/meltano/core/job_state.py:24`).

`Payload` adalah `IntEnum` dengan `STATE = 1` dan `INCOMPLETE_STATE = 2`
(`src/meltano/core/job/job.py:112-116`) - inilah mekanisme pemisahan "state final" vs "state sementara".

### 5.2 Bagaimana state dibaca

Alur pembacaan ada di `src/meltano/core/plugin/singer/tap.py:247-336`:

```python
# 1. selalu bersihkan state lama
state_path.unlink(missing_ok=True)

elt_context = plugin_invoker.context
if not elt_context or not elt_context.job:
    return                                  # di luar pipeline: tidak ada state
if elt_context.full_refresh:
    logger.info("Performing full refresh, ignoring state left behind...")
    return                                  # --full-refresh mengabaikan state
if custom_state_filename := plugin_invoker.plugin_config_extras["_state"]:
    shutil.copy(custom_state_path, state_path)   # --state=<file> manual
    return
if state := self.get_singer_state(project=..., session=..., job_name=elt_context.job.job_name):
    with state_path.open("w") as state_file:
        json.dump(state, state_file, indent=2)
else:
    logger.warning("No state was found, complete import.")
```

`get_singer_state` (`src/meltano/core/plugin/singer/tap.py:308-336`) mengambil dari
`StateService.get_state(job_name)` dan mengembalikan `state.get(SINGER_STATE_KEY)`.

### 5.3 Bagaimana state ditulis (checkpoint)

`BookmarkWriter.writeline` (`src/meltano/core/plugin/singer/target.py:66-121`) adalah mekanisme checkpoint
sesungguhnya:

```python
if self.job is None:
    logger.info("Running outside a Job context: incremental state could not be updated.")
    return
new_state = json.loads(line)          # gagal parse -> warning + return (state TIDAK di-update)
if not new_state:                     # state kosong -> tidak menimpa state sebelumnya
    logger.warning("Received empty state, incremental state has not been updated")
    return
job.payload[SINGER_STATE_KEY] = new_state
job.payload_flags = Payload(max(self.payload_flag, job.payload_flags))
job.save(self.session)                # 1) tulis ke tabel runs
self.state_service.add_state(job, json.dumps(job.payload), job.payload_flags)  # 2) tulis ke state store
```

Writer ini dipasang sebagai output handler pada stdout invoker
(`src/meltano/core/plugin/singer/target.py:181-209`):

```python
payload_flag = Payload.INCOMPLETE_STATE if elt_context.should_merge_states() else Payload.STATE
plugin_invoker.add_output_handler(plugin_invoker.StdioSource.STDOUT, BookmarkWriter(...))
```

`should_merge_states` (`src/meltano/core/_protocols/el_context.py:15-20`):

```python
return (self.full_refresh is True and self.state_strategy == StateStrategy.auto) or self.state_strategy == StateStrategy.merge
```

**Kesimpulan checkpoint:** granularitas = per pesan STATE dari target. Tidak ada checkpoint berbasis
baris/commit/batch. Strategi `merge` menandai state sebagai partial agar tidak menimpa completed state.

### 5.4 Bagaimana state dipersist

`StateService.add_state` (`src/meltano/core/state_service.py:153-195`):

```python
new_state_dict = json.loads(new_state)
if validate: self.validate_state(new_state_dict)   # wajib ada key "singer_state"
state_to_add_to.payload = new_state_dict
state_to_add_to.payload_flags = payload_flags
state_to_add_to.save(self.session)                 # tabel runs
partial_state   = new_state_dict if payload_flags == Payload.INCOMPLETE_STATE else {}
completed_state = new_state_dict if payload_flags == Payload.STATE else {}
job_state = MeltanoState(state_id=..., partial_state=..., completed_state=...)
self.state_store_manager.update(job_state)         # -> StatePersistenceError bila gagal
```

`StateStoreManager.update` bersifat `@t.final` (`src/meltano/core/state_store/base.py:150-165`) dan melakukan
**lock -> merge partial -> set**:

```python
with self.acquire_lock(state.state_id, retry_seconds=1):
    if not state.is_complete() and (current_state := self.get(state.state_id)):
        current_state.merge_partial(state)
        state_to_write = current_state
    self.set(state_to_write)
```

`get_state` mengembalikan `json_merged()` = partial di-merge di atas completed
(`src/meltano/core/state_store/base.py:92-98`, `src/meltano/core/state_service.py:197-209`).

Backend dipilih dari setting `state_backend.uri` (`src/meltano/core/state_store/__init__.py:105-132`),
default `systemdb` (`src/meltano/core/bundle/settings.yml:58-61`).

### 5.5 Locking

| Backend | Implementasi lock | Bukti |
|---|---|---|
| System DB | No-op - mengandalkan transaksi DB | `src/meltano/core/state_store/db.py:196-222` |
| Filesystem | File lock + polling `while self.is_locked(state_id): sleep(retry_seconds)` | `src/meltano/core/state_store/filesystem.py:275-295` |

Setting: `state_backend.lock_timeout_seconds` = 10, `state_backend.lock_retry_seconds` = 1
(`src/meltano/core/bundle/settings.yml:64-73`). Perhatikan: lock ini **naive lock berbasis timestamp**,
bukan distributed lock dengan fencing token - risiko yang harus diperhatikan (lihat bagian 11).

### 5.6 Penanganan kegagalan & recovery

Tiga mekanisme berbeda yang harus dibedakan:

**(a) Pencegahan run ganda (advisory, bukan lock).** `src/meltano/core/block/extract_load.py:527-538`:

```python
fail_stale_jobs(self.context.session, job.job_name)
if self.context.force:
    logger.warning("Force option is enabled, ignoring stale job check.")
elif existing := JobFinder(job.job_name).latest_running(self.context.session):
    raise RunnerError(f"Another '{job.job_name}' pipeline is already running which started at {existing.started_at}...")
```

Ini adalah **query database**, bukan penguncian resource. Dua proses berbeda host dengan system db yang sama
masih bisa lolos bersamaan bila timing-nya tepat.

**(b) Deteksi crash lewat heartbeat.** `src/meltano/core/job/job.py:40-41`, `205-230`:

```python
HEARTBEATLESS_JOB_VALID_HOURS = 24
HEARTBEAT_VALID_MINUTES = 5

def valid_until(self):
    if self.last_heartbeat_at:
        return self.last_heartbeat_at + timedelta(minutes=HEARTBEAT_VALID_MINUTES)
    return self.started_at + timedelta(hours=HEARTBEATLESS_JOB_VALID_HOURS)
```

`fail_stale_jobs` (`src/meltano/core/job/stale_job_failer.py:17-37`) -> `job.fail_stale()`
(`src/meltano/core/job/job.py:363-379`) menulis pesan: *"The process was likely killed unceremoniously."*
Heartbeat sendiri dijalankan oleh task asyncio `_heartbeater` dengan interval `job_heartbeat_interval`
(default 1 detik) (`src/meltano/core/job/job.py:411-446`, `src/meltano/core/bundle/settings.yml:30-33`);
validasi interval di `src/meltano/core/job/job.py:315-321`.

**(c) Resume.** Tidak ada mekanisme "resume run". Yang ada adalah **run baru dengan state lama**: job baru
dibuat, tap menerima `state.json` hasil run terakhir, replikasi berlanjut dari bookmark. Konsekuensi
langsung: **at-least-once, potensi duplikasi**. Kode tidak memiliki deduplikasi/upsert yang dikelola engine -
itu tanggung jawab target connector.

Tambahan penting: `JobState.from_job_history` (`src/meltano/core/job_state.py:66-105`) **bukan path runtime**.
Satu-satunya pemanggil di seluruh repo adalah migrasi
`src/meltano/migrations/versions/6828cc5b1a4f_create_dedicated_state_table.py:364`. Fungsinya merekonstruksi
state dari histori tabel `runs` (completed job terakhir + semua INCOMPLETE_STATE sesudahnya) saat migrasi
dari skema lama. Ini contoh perilaku yang mudah salah diasumsikan sebagai fitur aktif.

---

**Navigasi:** [Bagian 4](04-connector-dan-plugin.md) - [Daftar Isi](README.md) - [Bagian 6](06-subprocess-dan-reliability.md)
