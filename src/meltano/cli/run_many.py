"""Run independent Meltano jobs in parallel."""

from __future__ import annotations

import typing as t

import click

from meltano.cli.params import pass_project
from meltano.cli.utils import CliEnvironmentBehavior, CliError, PartialInstrumentedCmd
from meltano.core.pipeline_worker_service import PipelineWorkerService
from meltano.core.utils import run_async

if t.TYPE_CHECKING:
    from meltano.core.project import Project


@click.command(
    cls=PartialInstrumentedCmd,
    short_help="Run independent jobs in parallel worker processes.",
    environment_behavior=CliEnvironmentBehavior.environment_required,
)
@click.option(
    "--workers",
    "-w",
    "worker_count",
    type=click.IntRange(min=1),
    default=3,
    show_default=True,
    envvar="MELTANO_RUN_MANY_WORKERS",
    help="Maximum number of concurrent Meltano processes.",
)
@click.option(
    "--state-id-suffix-template",
    default="{job}",
    show_default=True,
    envvar="MELTANO_RUN_MANY_STATE_ID_SUFFIX_TEMPLATE",
    help="Unique state ID suffix template. The {job} placeholder is required "
    "to give each pipeline independent state.",
)
@click.option(
    "--dry-run",
    is_flag=True,
    help="Print the isolated Meltano commands without executing them.",
)
@click.argument("jobs", nargs=-1, required=True)
@pass_project(migrate=True)
@run_async
async def run_many(
    project: Project,
    *,
    worker_count: int,
    state_id_suffix_template: str,
    dry_run: bool,
    jobs: tuple[str, ...],
) -> None:
    """Run independent jobs concurrently in isolated Meltano processes.

    Plugins must already be installed. Run ``meltano install`` before using this
    command so concurrent workers never attempt to modify the same environment.
    """
    service = PipelineWorkerService(
        project,
        max_workers=worker_count,
        state_id_suffix_template=state_id_suffix_template,
    )
    tasks = service.plan(jobs)

    if dry_run:
        for task in tasks:
            click.echo(" ".join(service.command_for(task)))
        return

    click.echo(
        f"Running {len(tasks)} pipeline(s) with up to "
        f"{min(worker_count, len(tasks))} worker(s).",
    )
    results = await service.run(jobs)

    failed: list[str] = []
    for result in results:
        status = "SUCCESS" if result.succeeded else "FAILED"
        click.echo(
            f"{status} {result.job} "
            f"(worker {result.worker_id}, {result.duration_seconds:.1f}s)",
        )
        if not result.succeeded:
            failed.append(result.job)

    if failed:
        msg = f"{len(failed)} pipeline(s) failed: {', '.join(failed)}"
        raise CliError(
            msg,
        )
