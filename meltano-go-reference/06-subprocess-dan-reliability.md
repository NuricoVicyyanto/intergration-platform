> Bagian 6 dari 16 - [Daftar Isi](README.md)

## 6. Subprocess dan Reliability

### 6.1 Cara proses eksternal dijalankan

Single entry point: `PluginInvoker.invoke_async` (`src/meltano/core/plugin_invoker.py:516-539`) ->
`asyncio.create_subprocess_exec`.

```python
async with self._invoke(*args, **kwargs) as (popen_args, popen_options, popen_env):
    return await asyncio.create_subprocess_exec(*popen_args, **popen_options, env=popen_env)
```

Env dibangun di `PluginInvoker.env()` (`src/meltano/core/plugin_invoker.py:375-453`) dengan urutan merge yang
deterministik:

```python
env = {
    **self.plugin.exec_env(self),
    **expanded_project_env, **self.project.dotenv_env,
    **self.settings_service.env, **self.plugin_config_env,
    **expanded_root_plugin_env, **expanded_active_env,
    **expanded_active_env_plugin_env, **self.tracker.env,
}
env.pop("VIRTUAL_ENV", None); env.pop("PYTHONPATH", None)   # isolasi dari venv Meltano
if self.venv:
    env["VIRTUAL_ENV"] = str(self.venv.root)
    env["PATH"] = os.pathsep.join([venv_dir, env["PATH"]])
```

Ada mode strict env var (`FeatureFlags.STRICT_ENV_VAR_MODE`, `src/meltano/core/plugin_invoker.py:381-399`).
Jika executable hilang, `ExecutableNotFoundError` diproduksi dengan instruksi perbaikan
(`src/meltano/core/plugin_invoker.py:107-125`).

Varian lain: `invoke_docker` (`src/meltano/core/plugin_invoker.py:541-576`) menggunakan `ContainerService` dan
membaca `info["State"]["ExitCode"]`. Dan `MeltanoInvoker.invoke` memakai `subprocess.run` sync untuk memanggil
ulang `meltano` (`src/meltano/core/meltano_invoker.py:35-57`) - dipakai scheduler
(`src/meltano/core/schedule_service.py:253-288`).

### 6.2 Lifecycle

| Fase | Implementasi |
|---|---|
| prepare | `invoker.prepare(session)` menulis config + trigger hook `configure` (`plugin_invoker.py:257-279`) |
| start | `SingerBlock.start` (`block/singer.py:314-332`) |
| link | `_link_io` (`block/extract_load.py:638`) |
| wait | `process_future` = `ensure_future(process_handle.wait())` (`block/singer.py:178-183`) |
| stop | `kill()` atau `terminate()` lalu `await process_future` (`block/singer.py:334-358`) |
| cleanup | `InvokerBase.post()` -> `invoker.cleanup()` dengan `suppress(FileNotFoundError)` (`block/singer.py:233-238`) |

Catatan reliability yang terbukti dari komentar kode di `src/meltano/core/block/singer.py:235-237`:

```python
with suppress(FileNotFoundError):
    # TODO: should we preserve these on a failure?
    await self.invoker.cleanup()
```

Yaitu: file config/state di run dir **dihapus setelah run**, termasuk setelah kegagalan (TODO di kode
mengonfirmasi ini belum diputuskan). Untuk debugging, ini membatasi forensik pasca-kegagalan - hanya log yang
tersisa.

### 6.3 Exit code

`_check_exit_codes` (`src/meltano/core/block/extract_load.py:857-891`) memetakan exit code ke `RunnerError`
dengan dict `{PluginType: code}`:

```python
if producer_code:
    if consumer_code: raise RunnerError("Extractor and loader failed", {PluginType.EXTRACTORS: producer_code, PluginType.LOADERS: consumer_code})
    raise RunnerError("Extractor failed", {PluginType.EXTRACTORS: producer_code})
if consumer_code: raise RunnerError("Loader failed", {PluginType.LOADERS: consumer_code})
if failed_mappers := [...]: raise RunnerError("Mappers failed", failed_mappers)
```

Legacy: `src/meltano/core/runner/singer.py:199-207`. Pemetaan ke pesan pengguna ada di
`src/meltano/cli/elt.py:630-639` (`err.exitcodes[PluginType.EXTRACTORS]`), dan CLI keluar dengan `ctx.exit(1)`
pada `src/meltano/cli/run.py:311`.

### 6.4 Timeout & cancellation

- **Timeout**: hanya di `meltano run` via `--timeout` (detik) -> `asyncio.wait_for`
  (`src/meltano/cli/run.py:226-249`). Pada `asyncio.TimeoutError` -> log + `ctx.exit(1)`.
- **Graceful termination**: pada `asyncio.CancelledError`, `_run_blocks` memanggil
  `current_block.terminate(graceful=True)` (`src/meltano/cli/run.py:312-322`) ->
  `asyncio.gather(*(block.stop(kill=not graceful) ...))` (`src/meltano/core/block/extract_load.py:549-561`) ->
  `terminate()` = SIGTERM, `kill()` = SIGKILL.
- **SIGTERM -> CancelledError**: `_handling_sigterm` memasang
  `loop.add_signal_handler(signal.SIGTERM, cancel_task)` agar termination mengikuti jalur cancellation yang
  sama dengan SIGINT, memberi subprocess kesempatan shutdown rapi
  (`src/meltano/core/job/job.py:448-496`, dengan referensi issue #9981 di komentar).
- **Exit code 143**: `SIGTERM_EXIT_CODE = 143  # 128 + SIGTERM's signal number (15)`
  (`src/meltano/core/job/job.py:43`).

### 6.5 Retry - apa yang benar-benar ada

Hasil grep menyeluruh pada `src/meltano/core` dan `src/meltano/cli`:

| Retry | Lokasi | Parameter |
|---|---|---|
| Koneksi system DB | `src/meltano/core/db.py:126-160` | `database_max_retries`=3, `database_retry_timeout`=5s |
| Lock state filesystem | `src/meltano/core/state_store/filesystem.py:289-290` | `state_backend.lock_retry_seconds`=1 |
| Telemetry request | `src/meltano/core/tracking/tracker.py:138` | 3x |

**Tidak ditemukan retry untuk eksekusi tap/target.** Ini fakta penting: kegagalan koneksi ke sumber data yang
bersifat transien (network blip di tengah sync) akan menggagalkan seluruh run tanpa percobaan ulang otomatis.

### 6.6 Concurrency

Satu-satunya kontrol konkurensi yang terbukti adalah di instalasi plugin: decorator `with_semaphore`
(`src/meltano/core/plugin_install_service.py:141`) dan `asyncio.Semaphore` dengan `parallelism`
(`src/meltano/core/plugin_install_service.py:187-203`).

Eksekusi pipeline **serial**: `_run_blocks` melakukan `for idx, blk in enumerate(parsed_blocks): await blk.run()`
(`src/meltano/cli/run.py:272-296`). Multi-pipeline paralel dicapai dengan menjalankan beberapa proses
`meltano run` (mis. dengan `--force` atau state ID berbeda), bukan oleh scheduler internal. Catatan:
`ScheduleService` tidak memiliki daemon - `run()` hanya memanggil `meltano run <job>` / `meltano elt` lewat
`MeltanoInvoker` (`src/meltano/core/schedule_service.py:253-288`).

### 6.7 Efek kegagalan satu tahap ke tahap berikutnya

Tiga skenario terbukti dari `ELBExecutionManager` (`src/meltano/core/block/extract_load.py:737-854`):

1. **Tail (target) selesai lebih dulu padahal upstream belum** -> Meltano berasumsi target gagal sebelum
   memproses semua output: `_stop_all_blocks` dikomentari sebagai `_complete_upstream` yang menghentikan
   upstream lalu **menetapkan producer code = 0** (baris 725-733). Artinya, ketika target crash lebih dulu,
   exit code tap disembunyikan dan hanya `consumer_code` dilaporkan. Legacy runner melakukan hal serupa:
   `p_tap.kill()`, `tap_code = 0` (`src/meltano/core/runner/singer.py:166-177`).
2. **Head (extractor) selesai lebih dulu (normal)** -> stdin target ditutup agar cascade selesai alami
   (`src/meltano/core/block/extract_load.py:830-844`).
3. **Intermediate (mapper) gagal** -> `RunnerError("Unexpected completion sequence... Intermediate block
   (likely a mapper) failed.")` dengan exitcodes `{EXTRACTORS: 1, LOADERS: 1}` (baris 802-814).

### 6.8 Limit ukuran pesan

`elt.buffer_size` = 104,857,600 byte (100 MiB) (`src/meltano/core/bundle/settings.yml:46-49`). Limit baris =
`buffer_size // 2` (`src/meltano/core/block/singer.py:320-321`), karena StreamReader memakai limit sebagai
setengah buffer. Pelanggaran menghasilkan `asyncio.LimitOverrunError` yang diterjemahkan menjadi pesan
actionable (`src/meltano/core/block/future_utils.py:44-84`,
`src/meltano/core/runner/singer.py:246-275`). Ini konsekuensi langsung dari arsitektur line-delimited:
**satu RECORD besar dapat mematikan pipeline**.

### 6.9 Logging & observability

- Konfigurasi: `setup_logging` (`src/meltano/core/logging/utils.py:235-294`); format
  `colored|uncolored|json|key_value|plain` (`src/meltano/core/logging/utils.py:100-107`).
- Stdout/stderr subprocess -> structlog line-by-line: `OutputLogger.out(...)`
  (`src/meltano/core/logging/output_logger.py:45-76`), `Out.writeline`
  (`src/meltano/core/logging/output_logger.py:282-320`) dengan dukungan **log parser** untuk plugin
  ber-capability `structured-logging` (`src/meltano/core/plugin_invoker.py:650-661`).
- File log per job: `JobLoggingService.generate_log_name(job_name, run_id)`
  (`src/meltano/cli/elt.py:489-493`, `src/meltano/core/block/extract_load.py:370-375`); API akses log di
  `src/meltano/core/logging/job_logging_service.py:97-160`.
- Mode debug menyertakan stdout mentah plugin (`src/meltano/cli/elt.py:591-609`).
- Observability struktural: `run_id` di-bind ke contextvars (`structlog.contextvars.bind_contextvars`) di
  `src/meltano/cli/run.py:181-183` dan `src/meltano/cli/elt.py:341-343`, sehingga setiap log line membawa
  `run_id`, dan blok ELB membawa `job_name`, `cmd_type`, `producer`/`consumer`
  (`src/meltano/core/block/extract_load.py:648-656`).
- Telemetri: `Tracker`/Snowplow (`src/meltano/cli/cli.py:160-161`, `src/meltano/cli/run.py:276-277`).
- Query status job: `meltano job list` (`src/meltano/cli/job.py:144`) dan
  `JobLoggingService.get_recent_jobs` (`src/meltano/core/logging/job_logging_service.py:252`).

---

**Navigasi:** [Bagian 5](05-state-checkpoint-recovery.md) - [Daftar Isi](README.md) - [Bagian 7](07-diagram-arsitektur.md)
