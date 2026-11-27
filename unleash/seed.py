"""Seed the local Unleash with the fixture flags in fixtures.json.

The seed reconciles: it creates what is missing and replaces what differs from the fixtures, and it leaves
anything already matching untouched, so it can run any number of times. It uses only the standard library and
the Admin API of the Unleash version pinned in docker-compose.yaml.

Environment: UNLEASH_URL (default http://localhost:4242), UNLEASH_ADMIN_TOKEN (default the compose admin token)
and FIXTURES (default fixtures.json next to this script). It exits 1 on any API error.
"""

import json
import os
import pathlib
import sys
import urllib.error
import urllib.parse
import urllib.request

URL = os.environ.get("UNLEASH_URL", "http://localhost:4242").rstrip("/")
TOKEN = os.environ.get("UNLEASH_ADMIN_TOKEN", "*:*.releash-dev-admin-not-a-secret")
FIXTURES = pathlib.Path(os.environ.get("FIXTURES", pathlib.Path(__file__).with_name("fixtures.json")))


class APIError(Exception):
    pass


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
        raise APIError(f"{method} {path}: HTTP {e.code}: {e.read().decode(errors='replace')}") from None
    except urllib.error.URLError as e:
        raise APIError(f"{method} {path}: {e.reason}") from None
    return json.loads(raw) if raw else None


def quote(s):
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
    return (s["name"], tuple(sorted(params.items())), tuple(constraints))


def seed_context_field(field):
    name = field["name"]
    body = {
        "name": name,
        "description": field.get("description", ""),
        "legalValues": [{"value": v} for v in field.get("legalValues", [])],
        "stickiness": False,
    }
    current = api("GET", f"/api/admin/context/{quote(name)}", missing_ok=True)
    if current is None:
        api("POST", "/api/admin/context", body)
        print(f"context field {name}: created")
        return
    same = current.get("description", "") == body["description"] and sorted(
        v["value"] for v in current.get("legalValues", [])
    ) == sorted(field.get("legalValues", []))
    if same:
        print(f"context field {name}: unchanged")
        return
    api("PUT", f"/api/admin/context/{quote(name)}", body)
    print(f"context field {name}: updated")


def seed_flag(project, flag):
    name = flag["name"]
    base = f"/api/admin/projects/{quote(project)}/features/{quote(name)}"
    changes = []

    current = api("GET", base, missing_ok=True)
    flag_type = flag.get("type", "release")
    if current is None:
        api(
            "POST",
            f"/api/admin/projects/{quote(project)}/features",
            {"name": name, "type": flag_type, "description": flag.get("description", "")},
        )
        changes.append("created")
        current = api("GET", base)
    elif current.get("description", "") != flag.get("description", "") or current.get("type") != flag_type:
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
        if bool(envs[env].get("enabled")) != enabled:
            api("POST", f"{base}/environments/{quote(env)}/{'on' if enabled else 'off'}")
            changes.append(f"{env} {'on' if enabled else 'off'}")

    print(f"flag {name}: {', '.join(changes) if changes else 'unchanged'}")


def main():
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
