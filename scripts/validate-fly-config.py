#!/usr/bin/env python3
"""Validate the production Fly configuration without requiring Fly credentials."""

from __future__ import annotations

import sys
import tomllib
from pathlib import Path
from urllib.parse import urlparse

CONFIG_PATH = Path("fly.toml")
EXPECTED_APP = "albion-market-api-nachodev"
EXPECTED_REGION = "gru"
EXPECTED_FRONTEND_ORIGIN = "https://albion-production-calculator.pages.dev"


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def require_duration(value: object, field: str) -> None:
    require(isinstance(value, str) and value.strip(), f"{field} must be a duration string")
    require(value[-1] in {"s", "m", "h"}, f"{field} must include a duration unit")


def main() -> int:
    try:
        with CONFIG_PATH.open("rb") as config_file:
            config = tomllib.load(config_file)

        require(config.get("app") == EXPECTED_APP, "app name does not match the canonical domain")
        require(config.get("primary_region") == EXPECTED_REGION, "primary_region must be gru")
        require(config.get("kill_signal") == "SIGTERM", "kill_signal must be SIGTERM")

        kill_timeout = config.get("kill_timeout")
        require(isinstance(kill_timeout, int), "kill_timeout must be an integer number of seconds")
        require(1 <= kill_timeout <= 300, "kill_timeout must be between 1 and 300 seconds")

        build = config.get("build")
        require(isinstance(build, dict), "missing [build] section")
        require(build.get("dockerfile") == "Dockerfile", "build.dockerfile must be Dockerfile")

        deploy = config.get("deploy")
        require(isinstance(deploy, dict), "missing [deploy] section")
        require(
            deploy.get("release_command") == "/usr/local/bin/migrate",
            "deploy.release_command must execute the migration runner",
        )
        require_duration(deploy.get("release_command_timeout"), "deploy.release_command_timeout")
        require_duration(deploy.get("wait_timeout"), "deploy.wait_timeout")
        require(deploy.get("strategy") in {"rolling", "canary", "bluegreen"}, "unsupported deploy strategy")

        environment = config.get("env")
        require(isinstance(environment, dict), "missing [env] section")
        require(environment.get("APP_ENV") == "production", "APP_ENV must be production")
        require(environment.get("INGEST_REQUIRE_HTTPS") == "true", "ingest HTTPS must be required")
        require(environment.get("TRUST_PROXY_HEADERS") == "true", "Fly proxy headers must be trusted")
        require(environment.get("LOAD_DOTENV") == "false", "dotenv loading must be disabled")
        require(environment.get("LOG_FORMAT") == "json", "production logs must use JSON")
        require(
            environment.get("CORS_ALLOWED_ORIGINS") == EXPECTED_FRONTEND_ORIGIN,
            "CORS origin does not match the canonical frontend domain",
        )

        for forbidden_key in ("DATABASE_URL", "INGEST_BEARER_TOKEN", "FLY_API_TOKEN"):
            require(forbidden_key not in environment, f"secret {forbidden_key} must not be stored in fly.toml")

        origin = urlparse(str(environment["CORS_ALLOWED_ORIGINS"]))
        require(origin.scheme == "https" and bool(origin.netloc), "CORS origin must be an absolute HTTPS URL")

        http_service = config.get("http_service")
        require(isinstance(http_service, dict), "missing [http_service] section")
        require(http_service.get("internal_port") == 8080, "http_service.internal_port must be 8080")
        require(http_service.get("force_https") is True, "http_service.force_https must be true")
        require(http_service.get("auto_stop_machines") == "off", "production Machines must stay running")
        require(http_service.get("auto_start_machines") is True, "auto_start_machines must be true")
        require(http_service.get("min_machines_running") == 1, "one Machine must remain running")

        concurrency = http_service.get("concurrency")
        require(isinstance(concurrency, dict), "missing http_service.concurrency")
        require(concurrency.get("type") == "requests", "concurrency type must be requests")
        require(
            isinstance(concurrency.get("soft_limit"), int)
            and isinstance(concurrency.get("hard_limit"), int)
            and concurrency["soft_limit"] < concurrency["hard_limit"],
            "concurrency limits must be integers with soft_limit below hard_limit",
        )

        checks = http_service.get("checks")
        require(isinstance(checks, list) and len(checks) == 1, "exactly one HTTP service check is required")
        readiness = checks[0]
        require(readiness.get("path") == "/readyz", "routing check must use /readyz")
        require(readiness.get("method") == "GET", "routing check must use GET")
        require(readiness.get("protocol") == "http", "internal routing check must use HTTP")
        require(
            readiness.get("headers", {}).get("X-Forwarded-Proto") == "https",
            "routing check must identify the original protocol as HTTPS",
        )

        machines = config.get("vm")
        require(isinstance(machines, list) and len(machines) == 1, "exactly one [[vm]] profile is required")
        machine = machines[0]
        require(machine.get("size") == "shared-cpu-1x", "unexpected Fly Machine size")
        require(machine.get("memory") == "512mb", "unexpected Fly Machine memory")

    except (OSError, tomllib.TOMLDecodeError, KeyError, TypeError, ValueError) as error:
        print(f"Fly configuration validation failed: {error}", file=sys.stderr)
        return 1

    print("Fly production configuration contract is valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
