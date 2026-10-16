> Bagian 7 dari 16 - [Daftar Isi](README.md)

## 7. Diagram Arsitektur dan Sequence

### 7.1 Flowchart eksekusi aktual (`meltano run`)

```mermaid
flowchart TD
    A["meltano run tap-x target-y<br/>cli/run.py:137 run()"] --> B["BlockParser<br/>block_parser.py:78"]
    B --> B1["_expand_jobs: job/task-set to flat args<br/>block_parser.py:168"]
    B1 --> C["find_blocks / _find_next_elb_set<br/>block_parser.py:195,237"]
    C --> D{"validate_block_sets<br/>block_parser.py:54"}
    D -->|gagal| D1["CliError + ctx.exit<br/>cli/run.py:214-218"]
    D -->|lolos| E["ExtractLoadBlocks.validate_set<br/>extract_load.py:468"]
    E --> F["install_plugins<br/>plugin_install_service.py:286"]
    F --> G["_run_blocks: await blk.run()<br/>cli/run.py:264"]
    G --> H{"Environment aktif?<br/>extract_load.py:356"}
    H -->|tidak| H1["stateless run, job=None<br/>extract_load.py:504-510"]
    H -->|ya| I["state_id = env:head-to-tail<br/>extract_load.py:360,894"]
    I --> J["Job.run ctx: RUNNING + heartbeat<br/>job/job.py:292-339"]
    J --> K["execute() -> _start_blocks<br/>extract_load.py:489,619"]
    K --> K1["block.pre -> invoker.prepare<br/>plugin_invoker.py:257"]
    K1 --> K2["hook before_invoke:<br/>tap.look_up_state / target.setup_bookmark_writer"]
    K2 --> L["SingerBlock.start -> create_subprocess_exec<br/>block/singer.py:314 / plugin_invoker.py:516"]
    L --> M["_link_io: tap.stdout -> target.stdin<br/>extract_load.py:638-681"]
    M --> N["ELBExecutionManager.run<br/>extract_load.py:703"]
    N --> O["_check_exit_codes<br/>extract_load.py:857"]
    O --> P{"exit code != 0?"}
    P -->|ya| P1["RunnerError -> Job FAIL<br/>job/job.py:333-339"]
    P -->|tidak| P2["Job SUCCESS + save<br/>job/job.py:331-332"]
    N --> Q["BookmarkWriter per STATE line<br/>target.py:66-121"]
    Q --> R["StateService.add_state<br/>state_service.py:153"]
    R --> S["StateStoreManager.update:<br/>lock -> merge partial -> set<br/>state_store/base.py:150"]
    S --> T["DBStateStoreManager / Filesystem / S3 / GCS / Azure"]
```

Diagram ini sengaja menyimpang dari asumsi umum: **state ditulis di dalam loop eksekusi (setiap STATE),
bukan sebagai tahap terpisah setelah pemuatan selesai.** Penyelesaian job (`SUCCESS`) dan penyimpanan state
adalah dua jalur paralel yang tidak transaksional.

### 7.2 Sequence diagram

```mermaid
sequenceDiagram
    autonumber
    participant U as "User / CI"
    participant CLI as "cli/run.py"
    participant BP as "BlockParser"
    participant ELB as "ExtractLoadBlocks"
    participant JOB as "Job (runs table)"
    participant TAP as "tap subprocess"
    participant TGT as "target subprocess"
    participant SS as "StateService + StateStore"

    U->>CLI: meltano run tap-x target-y
    CLI->>BP: BlockParser(project, blocks, ...)
    BP->>BP: _expand_jobs (task set -> args)
    BP->>BP: find_blocks + validate_block_sets
    BP-->>CLI: [ExtractLoadBlocks]
    CLI->>CLI: install_plugins (semaphore)
    CLI->>ELB: blk.run()
    ELB->>ELB: generate_state_id(env, head, tail)
    ELB->>JOB: Job.run: start() + save() [RUNNING]
    Note over ELB,JOB: heartbeat tiap job_heartbeat_interval detik
    ELB->>TAP: prepare() + hook before_invoke
    TAP->>SS: get_state(state_id)
    SS-->>TAP: {"singer_state": {...}}
    TAP->>TAP: tulis state.json di run dir
    ELB->>TAP: spawn --config ... --state state.json
    ELB->>TGT: spawn (stdin=PIPE)
    ELB->>ELB: _link_io: tap.stdout -> tgt.stdin
    loop per baris Singer
        TAP-->>TGT: SCHEMA / RECORD / STATE
        TGT->>TGT: tulis ke destination
        TGT-->>SS: BookmarkWriter.writeline(STATE)
        SS->>SS: acquire_lock + merge partial + set
    end
    TAP-->>ELB: exit 0
    ELB->>TGT: close_stdin()
    TGT-->>ELB: exit 0
    ELB->>ELB: _check_exit_codes
    ELB->>JOB: success() + save() [SUCCESS]
    ELB->>TAP: post() -> invoker.cleanup()
```

### 7.3 Relasi antarmodul kunci (ringkas)

| Dari | Ke | Melalui |
|---|---|---|
| `cli/run.py` | `block/block_parser.py` | `BlockParser`, `validate_block_sets` |
| `block/block_parser.py` | `block/extract_load.py` | `ELBContextBuilder`, `ExtractLoadBlocks` |
| `block/extract_load.py` | `block/singer.py` | `SingerBlock` di `make_block` |
| `block/singer.py` | `plugin_invoker.py` | `invoke_async`, `StdioSource` |
| `plugin_invoker.py` | `plugin/singer/{tap,target}.py` | hooks `before_invoke` |
| `plugin/singer/target.py` | `state_service.py` | `BookmarkWriter` -> `add_state` |
| `state_service.py` | `state_store/` | `state_store_manager_from_project_settings` |
| `plugin/singer/tap.py` | `state_service.py` | `get_singer_state` |
| `block/extract_load.py` | `job/job.py` | `Job.run` ctx manager |
| `cli/elt.py` | `runner/singer.py` | `SingerRunner` (jalur legacy) |

---

**Navigasi:** [Bagian 6](06-subprocess-dan-reliability.md) - [Daftar Isi](README.md) - [Bagian 8](08-pemetaan-ke-golang.md)
