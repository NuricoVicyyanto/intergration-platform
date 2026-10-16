> Bagian 4 dari 16 - [Daftar Isi](README.md)

## 4. Mekanisme Connector dan Plugin

### 4.1 Registrasi & resolusi definisi plugin

`ProjectPluginsService` (`src/meltano/core/project_plugins_service.py:114`) adalah sumber kebenaran. Resolusi
parent memakai urutan eksplisit `DefinitionSource` (`src/meltano/core/project_plugins_service.py:35`):

```python
# project_plugins_service.py:520-564 (find_parent)
if plugin.inherit_from and not plugin.is_variant_set and self.supports_source(DefinitionSource.INHERITED):
    ... # 1. inheritance dari plugin lain di project
if self.supports_source(DefinitionSource.LOCKFILE):
    ... self.locked_definition_service.get_base_plugin(plugin, variant_name=plugin.variant)
```

Jadi definisi plugin berasal dari tiga lapis: **`meltano.yml` (project)** -> **`plugin.lock` (lockfile lokal,
`LockedDefinitionService`)** -> **Meltano Hub (remote, `hub/client.py`)**. `ensure_parent`
(`src/meltano/core/project_plugins_service.py:585-597`) mengisi `plugin.parent` secara lazy.

Kelas konkret per tipe dipilih oleh `base_plugin_factory` (`src/meltano/core/plugin/factory.py:47-65`):

```python
# plugin/factory.py:34-44
base_plugin_classes = {
    PluginType.EXTRACTORS:   lazy_import(".singer", "SingerTap"),
    PluginType.LOADERS:      lazy_import(".singer", "SingerTarget"),
    PluginType.TRANSFORMERS: lazy_import(".dbt", "DbtPlugin"),
    PluginType.MAPPERS:      lazy_import(".singer", "SingerMapper"),
    ...
}
```

`PluginType` didefinisikan di `src/meltano/core/plugin/base.py:73` sebagai `YAMLEnum`; `PluginRef` di baris
176; `Variant` di 247; `PluginDefinition` di 329; `BasePlugin` di 514; kontrak file plugin di `config_files`
(baris 713) dan `output_files` (baris 718).

### 4.2 Konfigurasi plugin -> file di disk

`PluginInvoker.prepare` (`src/meltano/core/plugin_invoker.py:257-279`) menjalankan empat hal:
`settings_service.as_dict(extras=False)`, `as_dict(process=True)`, `as_dict(extras=True)`, dan `as_env()`;
lalu trigger hook `"configure"` dan memanggil `PluginConfigService.configure()`.

`PluginConfigService.configure` (`src/meltano/core/plugin/config_service.py:32-56`) menyalin setiap
`plugin.config_files` (kecuali `"config"`) dari `config_dir` ke `run_dir`:

```python
stubs = ((run_file(f), config_file(f)) for file_id, f in self.plugin.config_files.items() if file_id != "config")
for dst, src in stubs:
    try: shutil.copy(src, dst)
    except FileNotFoundError: logger.debug(...)
```

Jadi **state masuk ke connector sebagai file**, bukan argumen panjang. Ini terlihat pada
`SingerTap.exec_args` (`src/meltano/core/plugin/singer/tap.py:170-204`):

```python
args = ["--config", plugin_invoker.files["config"]]
catalog_path = plugin_invoker.files["catalog"]
if file_has_data(catalog_path):
    if "catalog" in plugin_invoker.capabilities:   args += ["--catalog", catalog_path]
    elif "properties" in plugin_invoker.capabilities: args += ["--properties", catalog_path]
    else: logger.warning("...ignored as the extractor does not advertise the `catalog` or `properties` capability")
state_path = plugin_invoker.files["state"]
if file_has_data(state_path):
    if "state" in plugin_invoker.capabilities: args += ["--state", state_path]
    else: logger.warning("...does not advertise the `state` capability")
```

Nama file konkret (`src/meltano/core/plugin/singer/tap.py:206-223`): `tap.{instance_uuid}.config.json`,
`tap.properties.json`, `tap.properties.cache_key`, `state.json`, `tap.out`.

**Capabilities adalah gerbang fitur.** Contoh: `SingerBlock.has_state` =
`"state" in self.invoker.capabilities` (`src/meltano/core/block/singer.py:305-311`). Ini artinya sistem
incremental sepenuhnya bergantung pada deklarasi connector, bukan pada deteksi runtime.

### 4.3 Batas engine <-> proses connector

Batas ini sangat tajam dan penting untuk desain Golang:

| Aspek | Mekanisme terbukti | Lokasi |
|---|---|---|
| Konfigurasi | file JSON di run dir | `plugin/config_service.py:39-54` |
| Environment | dict gabungan 9 lapis (project -> active env -> plugin -> plugin_config_env -> tracker) | `plugin_invoker.py:429-453` |
| Argv | `exec_args()` per tipe plugin | `plugin_invoker.py:330-356` |
| Data engine->connector | `stdin=PIPE` (Singer messages) | `block/singer.py:323-330` |
| Data connector->engine | `stdout` = Singer messages **atau** state | `block/singer.py:327-329` |
| Log | `stderr` | `block/singer.py:329` |
| Perubahan behavior | **hooks**, bukan interface | `plugin/base.py` (`HookObject`), `tap.py:225` |

Mekanisme hooks adalah inti: `SingerTap.look_up_state_hook` (`src/meltano/core/plugin/singer/tap.py:225`) dan
`SingerTarget.setup_bookmark_writer_hook` (`src/meltano/core/plugin/singer/target.py:162`) dijalankan oleh
`PluginInvoker._invoke` melalui `self.plugin.trigger_hooks("invoke", self, args)`
(`src/meltano/core/plugin_invoker.py:495`). **Ini menggantikan dependency injection**: engine menyerahkan
perilaku ke plugin.

### 4.4 Orkestrasi ekstraksi -> pemuatan

`ExtractLoadBlocks._link_io` (`src/meltano/core/block/extract_load.py:638-681`) adalah tempat "pipeline"
benar-benar dibangun:

```python
if block.consumer and block.stdin is not None:
    if idx != 0 and self.blocks[idx - 1].producer:
        self.blocks[idx - 1].stdout_link(block.stdin)   # tap.stdout -> target.stdin
    else:
        raise BlockSetValidationError("run step requires input but has no upstream")
```

Prosesor tunggal yang membaca dan menulis adalah `capture_subprocess_output`
(`src/meltano/core/logging/utils.py:341-367`):

```python
while reader and not reader.at_eof():
    line = await reader.readline()
    for writer in line_writers:
        if not await _write_line_writer(writer, line):
            return   # BrokenPipeError/ConnectionResetError -> berhenti menyalin
```

Manager eksekusi `ELBExecutionManager.run` (`src/meltano/core/block/extract_load.py:684-854`) menunggu
`FIRST_COMPLETED` dari process futures dan output futures, lalu memutuskan siapa yang gagal.

---

**Navigasi:** [Bagian 3](03-alur-eksekusi-pipeline.md) - [Daftar Isi](README.md) - [Bagian 5](05-state-checkpoint-recovery.md)
