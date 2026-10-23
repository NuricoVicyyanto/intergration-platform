"""Run independent Meltano jobs in parallel worker processes."""

from __future__ import annotations

import asyncio
import sys
import time
import typing as t
from contextlib import suppress
from dataclasses import dataclass

import structlog

from meltano.core.constants import STATE_ID_COMPONENT_DELIMITER
from meltano.core.error import MeltanoError

if t.TYPE_CHECKING:
    from asyncio.subprocess import Process

    from meltano.core.project import Project


logger = structlog.stdlib.get_logger(__name__)


class PipelineWorkerError(MeltanoError):
    """Raised when a parallel pipeline run cannot be started safely."""


@dataclass(frozen=True, slots=True)
class PipelineWorkerTask:
    """A pipeline prepared for execution by a worker."""

    index: int
    job: str
    state_id_suffix: str


@dataclass(frozen=True, slots=True)
class PipelineWorkerResult:
    """The result of a pipeline worker execution."""

    job: str
    state_id_suffix: str
    worker_id: int
    return_code: int
    duration_seconds: float

    @property
    def succeeded(self) -> bool:
        """Whether the pipeline process exited successfully."""
        return self.return_code == 0


class PipelineWorkerService:
    """Run independent Meltano jobs in isolated subprocesses.

    Each job is executed by a separate ``meltano run`` process. This keeps
    project globals, SQLAlchemy sessions, signal handlers, and plugin
    subprocesses isolated from other pipelines.
    """

    def __init__(
        self,
        project: Project,
        *,
        max_workers: int = 3,
        state_id_suffix_template: str = "{job}",
        executable: t.Sequence[str] | None = None,
    ) -> None:
        """Create a pipeline worker service.

        Args:
            project: The Meltano project containing the jobs.
            max_workers: Maximum number of concurrent worker processes.
            state_id_suffix_template: Template used to generate a unique state
                ID suffix for each job. ``{job}`` is replaced with the job name.
            executable: Command used to invoke Meltano. Defaults to
                ``python -m meltano.cli`` from the current interpreter.

        Raises:
            PipelineWorkerError: If the worker or environment configuration is invalid.
        """
        if max_workers < 1:
            reason = "max_workers must be at least 1"
            instruction = "Set the worker count to a positive integer"
            raise PipelineWorkerError(
                reason,
                instruction,
            )
        if project.environment is None:
            reason = "Parallel pipeline runs require an active Meltano environment"
            instruction = (
                "Select an environment with --environment or configure "
                "default_environment"
            )
            raise PipelineWorkerError(
                reason,
                instruction,
            )

        self.project = project
        self.max_workers = max_workers
        self.state_id_suffix_template = state_id_suffix_template
        self.executable = tuple(
            executable or (sys.executable, "-m", "meltano.cli"),
        )
        self._active_processes: set[Process] = set()

    def plan(self, jobs: t.Sequence[str]) -> tuple[PipelineWorkerTask, ...]:
        """Validate jobs and assign unique state ID suffixes.

        Args:
            jobs: Names of jobs to run in parallel.

        Returns:
            Pipeline tasks ready for execution.

        Raises:
            PipelineWorkerError: If jobs or generated state ID suffixes are invalid.
        """
        if not jobs:
            reason = "No jobs were provided"
            instruction = "Pass one or more job names to meltano run-many"
            raise PipelineWorkerError(
                reason,
                instruction,
            )

        tasks: list[PipelineWorkerTask] = []
        seen_jobs: set[str] = set()
        seen_suffixes: set[str] = set()
        for index, job in enumerate(jobs, start=1):
            if not job:
                reason = "A job name cannot be empty"
                instruction = "Provide a named Meltano job"
                raise PipelineWorkerError(
                    reason,
                    instruction,
                )
            if job in seen_jobs:
                reason = f"Job '{job}' was provided more than once"
                instruction = "Provide each independent job only once"
                raise PipelineWorkerError(
                    reason,
                    instruction,
                )

            try:
                state_id_suffix = self.state_id_suffix_template.format(job=job)
            except (IndexError, KeyError, ValueError) as err:
                reason = "Invalid state ID suffix template"
                instruction = "Use a valid template containing the job placeholder"
                raise PipelineWorkerError(
                    reason,
                    instruction,
                ) from err

            if not state_id_suffix:
                reason = f"Generated an empty state ID suffix for job '{job}'"
                instruction = "Use a non-empty --state-id-suffix-template"
                raise PipelineWorkerError(
                    reason,
                    instruction,
                )
            if STATE_ID_COMPONENT_DELIMITER in state_id_suffix:
                reason = (
                    f"State ID suffix '{state_id_suffix}' contains "
                    f"the reserved delimiter '{STATE_ID_COMPONENT_DELIMITER}'"
                )
                instruction = "Choose a suffix template without reserved delimiters"
                raise PipelineWorkerError(
                    reason,
                    instruction,
                )
            if state_id_suffix in seen_suffixes:
                reason = f"State ID suffix '{state_id_suffix}' is not unique"
                instruction = (
                    "Use a template containing the job placeholder so each worker "
                    "gets its own state"
                )
                raise PipelineWorkerError(
                    reason,
                    instruction,
                )

            seen_jobs.add(job)
            seen_suffixes.add(state_id_suffix)
            tasks.append(
                PipelineWorkerTask(
                    index=index,
                    job=job,
                    state_id_suffix=state_id_suffix,
                ),
            )

        return tuple(tasks)

    def command_for(self, task: PipelineWorkerTask) -> list[str]:
        """Build the isolated Meltano command for a pipeline task.

        Args:
            task: The pipeline task to build a command for.

        Returns:
            The command and arguments passed to the child process.
        """
        assert self.project.environment  # noqa: S101
        return [
            *self.executable,
            "--cwd",
            str(self.project.root),
            "--environment",
            self.project.environment.name,
            "run",
            "--no-install",
            "--state-id-suffix",
            task.state_id_suffix,
            task.job,
        ]

    async def run(self, jobs: t.Sequence[str]) -> tuple[PipelineWorkerResult, ...]:
        """Run jobs concurrently using at most ``max_workers`` processes.

        Args:
            jobs: Names of independent jobs to run.

        Returns:
            Results ordered according to the input job order.
        """
        tasks = self.plan(jobs)
        queue: asyncio.Queue[PipelineWorkerTask | None] = asyncio.Queue()
        for task in tasks:
            queue.put_nowait(task)
        for _ in range(min(self.max_workers, len(tasks))):
            queue.put_nowait(None)

        results: list[PipelineWorkerResult | None] = [None] * len(tasks)
        worker_tasks = [
            asyncio.create_task(
                self._worker(
                    worker_id=worker_id,
                    queue=queue,
                    results=results,
                ),
            )
            for worker_id in range(1, min(self.max_workers, len(tasks)) + 1)
        ]

        try:
            await asyncio.gather(*worker_tasks)
        except BaseException:
            processes = tuple(self._active_processes)
            for process in processes:
                if process.returncode is None:
                    with suppress(ProcessLookupError):
                        process.terminate()
            for worker_task in worker_tasks:
                worker_task.cancel()
            await asyncio.gather(*worker_tasks, return_exceptions=True)
            await asyncio.gather(
                *(process.wait() for process in processes),
                return_exceptions=True,
            )
            raise

        if any(result is None for result in results):
            reason = "One or more workers did not return a result"
            instruction = "Retry the failed jobs after checking the worker logs"
            raise PipelineWorkerError(
                reason,
                instruction,
            )

        return t.cast("tuple[PipelineWorkerResult, ...]", tuple(results))

    async def _worker(
        self,
        *,
        worker_id: int,
        queue: asyncio.Queue[PipelineWorkerTask | None],
        results: list[PipelineWorkerResult | None],
    ) -> None:
        while True:
            task = await queue.get()
            if task is None:
                return

            results[task.index - 1] = await self._run_task(
                task=task,
                worker_id=worker_id,
            )

    async def _run_task(
        self,
        *,
        task: PipelineWorkerTask,
        worker_id: int,
    ) -> PipelineWorkerResult:
        command = self.command_for(task)
        logger.info(
            "Starting pipeline worker",
            job=task.job,
            worker=worker_id,
            state_id_suffix=task.state_id_suffix,
        )
        started_at = time.perf_counter()
        process = await asyncio.create_subprocess_exec(*command)
        self._active_processes.add(process)
        try:
            return_code = await process.wait()
        finally:
            self._active_processes.discard(process)

        duration_seconds = time.perf_counter() - started_at
        logger.info(
            "Pipeline worker completed",
            job=task.job,
            worker=worker_id,
            return_code=return_code,
            duration_seconds=round(duration_seconds, 3),
        )
        return PipelineWorkerResult(
            job=task.job,
            state_id_suffix=task.state_id_suffix,
            worker_id=worker_id,
            return_code=return_code,
            duration_seconds=duration_seconds,
        )
