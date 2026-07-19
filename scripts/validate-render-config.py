#!/usr/bin/env python3
"""Validate the repository contract for the Render production service."""

from __future__ import annotations

import re
import sys
from pathlib import Path

RENDER_CONFIG = Path("render.yaml")
DEPLOY_WORKFLOW = Path(".github/workflows/deploy-production.yml")
EXPECTED_API = "https://albion-market-api.onrender.com"
EXPECTED_FRONTEND = "https://albioncalculator.app"
LEGACY_FRONTEND = "https://albion-production-calculator.pages.dev"

RETIRED_PATHS = (
    Path("fly.toml"),
    Path("scripts/bootstrap-fly-production.ps1"),
    Path("scripts/validate-fly-config.py"),
    Path("docs/deployment/fly-neon-production.md"),
)


def require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def require_fragment(content: str, fragment: str, label: str) -> None:
    require(fragment in content, f"missing {label}: {fragment}")


def main() -> int:
    try:
        render = RENDER_CONFIG.read_text(encoding="utf-8")
        workflow = DEPLOY_WORKFLOW.read_text(encoding="utf-8")

        for fragment, label in (
            ("name: albion-market-api", "canonical service name"),
            ("runtime: docker", "Docker runtime"),
            ("branch: main", "production branch"),
            ("autoDeployTrigger: off", "disabled automatic deploys"),
            ("dockerfilePath: ./Dockerfile", "Dockerfile path"),
            ("dockerCommand: /usr/local/bin/albion-market-api", "runtime command"),
            ("healthCheckPath: /readyz", "readiness health check"),
            (f"value: {EXPECTED_FRONTEND}", "canonical CORS origin"),
            (LEGACY_FRONTEND, "legacy Pages compatibility origin"),
            (
                f"value: {EXPECTED_FRONTEND}/account?checkout=success",
                "canonical billing redirect",
            ),
            ("key: DATABASE_URL\n        sync: false", "Render database secret"),
            ("key: INGEST_BEARER_TOKEN\n        sync: false", "Render ingest secret"),
        ):
            require_fragment(render, fragment, label)

        require(
            not re.search(r"key:\s*(DATABASE_URL|INGEST_BEARER_TOKEN)\s*\n\s*value:", render),
            "runtime secrets must not have committed values",
        )

        for fragment, label in (
            ("name: Deploy production to Render", "workflow name"),
            ("environment: production", "protected GitHub Environment"),
            ("NEON_MIGRATION_DATABASE_URL", "direct Neon migration secret"),
            ("RENDER_DEPLOY_HOOK_URL", "Render deploy hook secret"),
            (EXPECTED_API, "canonical Render URL"),
            (EXPECTED_FRONTEND, "canonical Cloudflare URL"),
            ("/usr/local/bin/migrate", "migration runner"),
            ("albion_market_api_build_info", "exact revision probe"),
            ("/api/v1/ingest/prices", "non-writing authentication probe"),
            ("/api/v1/prices", "public prices probe"),
            ("/api/v1/history", "public history probe"),
        ):
            require_fragment(workflow, fragment, label)

        require(
            "FRONTEND_ORIGIN: https://albion-production-calculator.pages.dev"
            not in workflow,
            "deployment workflow still treats pages.dev as canonical frontend",
        )

        forbidden_workflow_terms = ("FLY_API_TOKEN", "flyctl", ".fly.dev")
        for term in forbidden_workflow_terms:
            require(term not in workflow, f"retired Fly dependency remains in workflow: {term}")

        for path in RETIRED_PATHS:
            require(not path.exists(), f"retired Fly path is still tracked: {path}")

    except (OSError, UnicodeError, ValueError) as error:
        print(f"Render production configuration validation failed: {error}", file=sys.stderr)
        return 1

    print("Render production configuration contract is valid.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
