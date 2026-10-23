from __future__ import annotations

from unittest import mock

from meltano.cli import cli
from meltano.cli.utils import CliError
from meltano.core.pipeline_worker_service import PipelineWorkerResult


def test_run_many_uses_three_workers(cli_runner, project) -> None:
    project.activate_environment("dev")
    results = (
        PipelineWorkerResult("customers", "customers", 1, 0, 1.0),
        PipelineWorkerResult("orders", "orders", 2, 0, 1.0),
        PipelineWorkerResult("invoices", "invoices", 3, 0, 1.0),
    )

    with mock.patch(
        "meltano.cli.run_many.PipelineWorkerService.run",
        new=mock.AsyncMock(return_value=results),
    ) as run_mock:
        result = cli_runner.invoke(
            cli,
            [
                "--environment=dev",
                "run-many",
                "--workers",
                "3",
                "customers",
                "orders",
                "invoices",
            ],
        )

    assert result.exit_code == 0, result.output
    assert "up to 3 worker(s)" in result.output
    run_mock.assert_awaited_once_with(("customers", "orders", "invoices"))


def test_run_many_reports_failed_jobs(cli_runner, project) -> None:
    project.activate_environment("dev")
    results = (
        PipelineWorkerResult("customers", "customers", 1, 0, 1.0),
        PipelineWorkerResult("orders", "orders", 2, 1, 1.0),
    )

    with mock.patch(
        "meltano.cli.run_many.PipelineWorkerService.run",
        new=mock.AsyncMock(return_value=results),
    ):
        result = cli_runner.invoke(
            cli,
            ["--environment=dev", "run-many", "customers", "orders"],
        )

    assert result.exit_code == 1
    assert "FAILED orders" in result.output
    assert isinstance(result.exception, CliError)
    assert str(result.exception) == "1 pipeline(s) failed: orders"
