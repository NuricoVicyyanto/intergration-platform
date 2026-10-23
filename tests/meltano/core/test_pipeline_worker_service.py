from __future__ import annotations

import asyncio
import typing as t
from unittest import mock

import pytest

from meltano.core.environment import Environment
from meltano.core.pipeline_worker_service import (
    PipelineWorkerError,
    PipelineWorkerService,
)
from meltano.core.project import Project

if t.TYPE_CHECKING:
    from pathlib import Path


@pytest.fixture
def worker_project(tmp_path: Path) -> Project:
    return Project(tmp_path, environment=Environment(name="dev"))


class FakeProcess:
    def __init__(
        self,
        *,
        delay: float,
        return_code: int,
        active: dict[str, int],
    ) -> None:
        self.delay = delay
        self._return_code = return_code
        self.returncode: int | None = None
        self.active = active

    async def wait(self) -> int:
        await asyncio.sleep(self.delay)
        self.returncode = self._return_code
        self.active["count"] -= 1
        return self.returncode

    def terminate(self) -> None:
        if self.returncode is None:
            self.returncode = -15
            self.active["count"] -= 1


def test_plan_uses_unique_job_state_suffixes(worker_project: Project) -> None:
    service = PipelineWorkerService(worker_project)

    tasks = service.plan(("customers", "orders", "invoices"))

    assert [task.state_id_suffix for task in tasks] == [
        "customers",
        "orders",
        "invoices",
    ]
    assert service.command_for(tasks[0])[-3:] == [
        "--state-id-suffix",
        "customers",
        "customers",
    ]


def test_plan_rejects_duplicate_state_suffixes(worker_project: Project) -> None:
    service = PipelineWorkerService(
        worker_project,
        state_id_suffix_template="shared",
    )

    with pytest.raises(PipelineWorkerError, match="not unique"):
        service.plan(("customers", "orders"))


@pytest.mark.asyncio
async def test_run_is_bounded_by_worker_count(worker_project: Project) -> None:
    active = {"count": 0, "max": 0}

    async def create_process(*args: str, **kwargs: t.Any) -> FakeProcess:
        del args, kwargs
        active["count"] += 1
        active["max"] = max(active["max"], active["count"])
        return FakeProcess(
            delay=0.02,
            return_code=0,
            active=active,
        )

    with mock.patch(
        "meltano.core.pipeline_worker_service.asyncio.create_subprocess_exec",
        side_effect=create_process,
    ):
        results = await PipelineWorkerService(
            worker_project,
            max_workers=3,
        ).run(("one", "two", "three", "four", "five"))

    assert active["max"] == 3
    assert len(results) == 5
    assert all(result.succeeded for result in results)
