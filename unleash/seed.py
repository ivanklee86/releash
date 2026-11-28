"""Seed the local Unleash with the fixture flags in fixtures.json.

The seed reconciles: it creates what is missing and replaces what differs from the fixtures, and it leaves
anything already matching untouched, so it can run any number of times. It never deletes or archives flags that are
no longer in the fixtures, so after removing or renaming a fixture, start from a clean stack with `task services:down`
and `task services`. It uses only the standard library and
the Admin API of the Unleash version pinned in docker-compose.yaml.

Environment: UNLEASH_SERVER_URL, the server root without /api (default http://localhost:4242), UNLEASH_ADMIN_TOKEN
(default the compose admin token) and FIXTURES (default fixtures.json next to this script). It exits 1 on any API
error.
"""

import json
import os
import pathlib
import sys
import urllib.error
import urllib.parse
import urllib.request

URL = os.environ.get("UNLEASH_SERVER_URL", "http://localhost:4242").rstrip("/")
TOKEN = os.environ.get("UNLEASH_ADMIN_TOKEN", "*:*.releash-dev-admin-not-a-secret")
FIXTURES = pathlib.Path(os.environ.get("FIXTURES", pathlib.Path(__file__).with_name("fixtures.json")))


class APIError(Exception):
    """An Admin API call failed. status and body are set when the server answered with an HTTP error."""

    def __init__(self, message, status=None, body=""):
        super().__init__(message)
        self.status = status
        self.body = body


def api(method, path, body=None, missing_ok=False):
    """Calls the Admin API and returns the decoded JSON body, or None for an empty body or a 404 when missing_ok."""
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(URL + path, data=data, method=method)
    req.add_header("Authorization", TOKEN)
    req.add_header("Accept", "application/json")
    if data is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            raw = resp.read()
    except urllib.error.HTTPError as e:
        if e.code == 404 and missing_ok:
            return None
        body = e.read().decode(errors="replace")
        raise APIError(f"{method} {path}: HTTP {e.code}: {body}", status=e.code, body=body) from None
    except urllib.error.URLError as e:
        raise APIError(f"{method} {path}: {e.reason}") from None
    except OSError as e:
        # A timeout or reset while reading the body, after urlopen has returned.
        raise APIError(f"{method} {path}: {e}") from None
    try:
        return json.loads(raw) if raw else None
    except ValueError as e:
        raise APIError(f"{method} {path}: response is not JSON: {e}") from None


def quote(s):
    """Escapes s for use as one path segment."""
    return urllib.parse.quote(s, safe="")


def normalize_strategy(s):
    """Reduces a strategy to the fields the fixtures set, with the server's defaults filled in, for comparison."""
    constraints = []
    for c in s.get("constraints") or []:
        constraints.append(
            (
                c["contextName"],
                c["operator"],
                tuple(sorted(c.get("values") or [])),
                c.get("value") or "",
                bool(c.get("inverted", False)),
                bool(c.get("caseInsensitive", False)),
            )
        )
    params = {k: str(v) for k, v in (s.get("parameters") or {}).items()}
    # Variants are compared as canonical JSON because their payloads are nested objects.
    variants = json.dumps(s.get("variants") or [], sort_keys=True)
    return (
        s["name"],
        tuple(sorted(params.items())),
        tuple(constraints),
        bool(s.get("disabled", False)),
        tuple(sorted(s.get("segments") or [])),
        variants,
    )


def normalize_variants(variants):
    """Reduces feature variants to the fields the fixtures set, sorted by name with the server's defaults filled in,
    for comparison. Unleash serves them sorted by name and stores an omitted stickiness as default."""
    out = []
    for v in variants or []:
        payload = v.get("payload")
        out.append(
            (
                v["name"],
                int(v.get("weight", 0)),
                v.get("weightType", "variable"),
                v.get("stickiness") or "default",
                None if payload is None else (str(payload.get("type", "")), str(payload.get("value", ""))),
            )
        )
    return sorted(out)


def is_archived_name_conflict(e):
    """Reports whether e is Unleash 8.3.1 refusing to create a flag because an archived flag has its name."""
    if e.status != 409:
        return False
    try:
        message = json.loads(e.body).get("message", "")
    except (ValueError, AttributeError):
        return False
    return "archived flag with that name already exists" in message


def seed_context_field(field):
    """Creates the custom context field, or updates its description, legal values and stickiness to match. Raises
    APIError."""
    name = field["name"]
    body = {
        "name": name,
        "description": field.get("description", ""),
        "legalValues": [{"value": v} for v in field.get("legalValues", [])],
        "stickiness": bool(field.get("stickiness", False)),
    }
    current = api("GET", f"/api/admin/context/{quote(name)}", missing_ok=True)
    if current is None:
        api("POST", "/api/admin/context", body)
        print(f"context field {name}: created")
        return
    same = (
        current.get("description", "") == body["description"]
        and sorted(v["value"] for v in current.get("legalValues", [])) == sorted(field.get("legalValues", []))
        and bool(current.get("stickiness", False)) == body["stickiness"]
    )
    if same:
        print(f"context field {name}: unchanged")
        return
    api("PUT", f"/api/admin/context/{quote(name)}", body)
    print(f"context field {name}: updated")


def seed_variants(base, env, variants):
    """Replaces the feature variants of the flag at base in env when they differ from variants, and returns whether it
    did. Raises APIError, also when Unleash stores something other than variants, e.g. because their variable weights
    are not the even split of 1000 that Unleash computes, which would make every run rewrite them."""
    path = f"{base}/environments/{quote(env)}/variants"
    desired = normalize_variants(variants)
    if normalize_variants(api("GET", path).get("variants")) == desired:
        return False
    # The per-environment PUT is refused with 403 on a flag that has no feature variants yet ("Environment variants
    # deprecated"), while variants-batch works on any flag and replaces the variants of the environments it names.
    api("PUT", f"{base}/variants-batch", {"variants": variants, "environments": [env]})
    stored = normalize_variants(api("GET", path).get("variants"))
    if stored != desired:
        raise APIError(f"{path}: Unleash stored {stored}, not the fixture's {desired}; give weights as Unleash stores them")
    return True


def seed_flag(project, flag):
    """Makes the flag in project match the fixture: creates or revives it, replaces strategies and feature variants
    that differ, and turns each listed environment on or off. Raises APIError, also when a listed environment does not exist."""
    name = flag["name"]
    base = f"/api/admin/projects/{quote(project)}/features/{quote(name)}"
    changes = []

    current = api("GET", base, missing_ok=True)
    flag_type = flag.get("type", "release")
    if current is None:
        try:
            api(
                "POST",
                f"/api/admin/projects/{quote(project)}/features",
                {"name": name, "type": flag_type, "description": flag.get("description", "")},
            )
            changes.append("created")
        except APIError as e:
            if not is_archived_name_conflict(e):
                raise
            # An archived flag keeps its name, so it is revived instead. Unleash revives it turned off in every
            # environment, and the reconciliation below puts the rest back.
            api("POST", f"/api/admin/archive/revive/{quote(name)}")
            changes.append("revived")
        current = api("GET", base)
    # A revived flag keeps the description and type it was archived with, so this also runs after a revive.
    if current.get("description", "") != flag.get("description", "") or current.get("type") != flag_type:
        api("PUT", base, {"description": flag.get("description", ""), "type": flag_type})
        changes.append("updated description")

    envs = {e["name"]: e for e in current.get("environments", [])}
    desired = sorted(normalize_strategy(s) for s in flag["strategies"])
    for env, enabled in flag["enabled"].items():
        if env not in envs:
            raise APIError(f"flag {name}: environment {env} does not exist in project {project}")
        strategies = envs[env].get("strategies") or []
        if sorted(normalize_strategy(s) for s in strategies) != desired:
            # Add before deleting, so an enabled environment never has zero strategies, which Unleash fills with
            # a default strategy that would turn the flag on for everyone.
            for s in flag["strategies"]:
                api("POST", f"{base}/environments/{quote(env)}/strategies", s)
            for s in strategies:
                api("DELETE", f"{base}/environments/{quote(env)}/strategies/{quote(s['id'])}")
            changes.append(f"{env} strategies")
        if seed_variants(base, env, flag.get("variants", [])):
            changes.append(f"{env} variants")
        if bool(envs[env].get("enabled")) != enabled:
            api("POST", f"{base}/environments/{quote(env)}/{'on' if enabled else 'off'}")
            changes.append(f"{env} {'on' if enabled else 'off'}")

    print(f"flag {name}: {', '.join(changes) if changes else 'unchanged'}")


def main():
    """Seeds every context field and flag in FIXTURES and returns the exit code: 0, or 1 on the first API error."""
    fixtures = json.loads(FIXTURES.read_text())
    project = fixtures.get("project", "default")
    try:
        for field in fixtures.get("contextFields", []):
            seed_context_field(field)
        for flag in fixtures["flags"]:
            seed_flag(project, flag)
    except APIError as e:
        print(f"seed failed: {e}", file=sys.stderr)
        return 1
    print(f"seeded {len(fixtures['flags'])} flags into {URL}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
