#!/usr/bin/env python3

from __future__ import annotations

import json
import os
import ssl
import urllib.request
from urllib.parse import urlparse


def require(condition: bool, message: str) -> None:
    if not condition:
        raise SystemExit(message)


def read_json(url: str) -> dict[str, object]:
    request = urllib.request.Request(url, headers={"Accept": "application/json"})
    with urllib.request.urlopen(
        request,
        timeout=10,
        context=ssl.create_default_context(),
    ) as response:
        require(response.status == 200, f"{url} returned HTTP {response.status}")
        return json.load(response)


def endpoint_host(value: object, field: str) -> str:
    require(isinstance(value, str) and bool(value), f"{field} is missing")
    parsed = urlparse(value)
    require(parsed.scheme == "https", f"{field} must use HTTPS")
    require(bool(parsed.netloc), f"{field} must contain a host")
    return parsed.netloc


def main() -> None:
    enabled_value = os.getenv("EXPECTED_AUTH_ENABLED", "false").strip().lower()
    issuer = os.getenv("EXPECTED_AUTH_ISSUER", "").strip()
    audience = os.getenv("EXPECTED_AUTH_AUDIENCE", "").strip()

    require(
        enabled_value in {"true", "false"},
        "EXPECTED_AUTH_ENABLED must be true or false",
    )

    if enabled_value == "false":
        print("Auth0 production activation is disabled.")
        return

    require(bool(issuer), "EXPECTED_AUTH_ISSUER is required when Auth0 is enabled")
    require(bool(audience), "EXPECTED_AUTH_AUDIENCE is required when Auth0 is enabled")

    issuer = issuer.rstrip("/") + "/"
    issuer_url = urlparse(issuer)
    require(issuer_url.scheme == "https", "EXPECTED_AUTH_ISSUER must use HTTPS")
    require(bool(issuer_url.netloc), "EXPECTED_AUTH_ISSUER must contain a host")

    audience_url = urlparse(audience)
    require(audience_url.scheme == "https", "EXPECTED_AUTH_AUDIENCE must use HTTPS")
    require(bool(audience_url.netloc), "EXPECTED_AUTH_AUDIENCE must contain a host")

    discovery = read_json(f"{issuer}.well-known/openid-configuration")
    require(discovery.get("issuer") == issuer, "Auth0 discovery issuer does not match")

    for field in ("authorization_endpoint", "token_endpoint", "jwks_uri"):
        require(
            endpoint_host(discovery.get(field), field) == issuer_url.netloc,
            f"{field} must use the configured Auth0 host",
        )

    jwks_uri = discovery.get("jwks_uri")
    require(isinstance(jwks_uri, str), "jwks_uri is missing")
    jwks = read_json(jwks_uri)
    keys = jwks.get("keys")
    require(isinstance(keys, list) and len(keys) > 0, "Auth0 JWKS contains no keys")

    rs256_keys = [
        key
        for key in keys
        if isinstance(key, dict)
        and key.get("kty") == "RSA"
        and key.get("alg") in {None, "RS256"}
    ]
    require(bool(rs256_keys), "Auth0 JWKS contains no RS256-compatible RSA key")

    print(f"Auth0 production configuration is valid for {issuer}")
    print(f"Audience: {audience}")


if __name__ == "__main__":
    main()
