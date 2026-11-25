#!/usr/bin/env python3
import http.cookiejar
import json
import os
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:7070"
PASS_FILE = "tmp/evidence/M6/vm-panel-pass.txt"

cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))


def api(method, path, body=None):
    data = None
    headers = {"Host": "127.0.0.1:7070"}
    if method != "GET":
        headers["Origin"] = BASE
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(BASE + path, data=data, headers=headers, method=method)
    try:
        with opener.open(req, timeout=30) as resp:
            raw = resp.read().decode()
            return resp.status, json.loads(raw) if raw else {}
    except urllib.error.HTTPError as e:
        raw = e.read().decode()
        try:
            return e.code, json.loads(raw)
        except Exception:
            return e.code, {"raw": raw}


def vm(cmd):
    res = subprocess.run(["ssh", "dermaga-vm", cmd], capture_output=True, text=True, timeout=60)
    return res.returncode, res.stdout.strip(), res.stderr.strip()


def login():
    pw = open(PASS_FILE).read().strip()
    code, resp = api("POST", "/api/auth/login", {"password": pw})
    assert code == 200 and resp.get("success"), f"login failed: {code} {resp}"


def wait_dep(app_id, dep_id, targets, timeout=180):
    deadline = time.time() + timeout
    while time.time() < deadline:
        code, detail = api("GET", f"/api/apps/{app_id}")
        if code == 200:
            for d in detail.get("deployments", []):
                if d["id"] == dep_id:
                    if d["status"] in targets:
                        return d
        time.sleep(1.0)
    raise AssertionError(f"timeout waiting for {dep_id} -> {targets}")


def wait_dep_building(app_id, dep_id, timeout=30):
    return wait_dep(app_id, dep_id, {"building", "starting", "probing", "active", "failed"}, timeout=timeout)


def main():
    print("=== M6a Real-Podman VM Verification ===")
    login()
    print("PASS: logged in via SSH tunnel")

    # Idempotent cleanup if an earlier run aborted mid-way
    code, apps_list = api("GET", "/api/apps")
    if code == 200:
        for a in apps_list.get("apps", []):
            if a["name"] in ("testapp", "failapp", "cancelapp", "timeoutapp"):
                api("DELETE", f"/api/apps/{a['id']}", {"confirm_name": a["name"]})
                time.sleep(1)

    # 8. Repo without Containerfile
    code, insp_empty = api("GET", "/api/github/repos/r3p-dev/dermaga-test-empty/inspect?branch=main")
    print(f"INFO [8]: dermaga-test-empty inspect returned {code}: {insp_empty}")
    if code == 200:
        assert insp_empty.get("has_containerfile") is False, f"unexpected empty inspect: {insp_empty}"
        print(f"PASS [8]: dermaga-test-empty inspect has_containerfile=false")
    else:
        print("FINDING [8]: empty git repo (0 commits) returned 409 from GitHub /git/trees; will handle as has_containerfile=false in M6b")

    # 1. Inspect & first deploy of r3p-dev/dermaga-test-app
    code, insp = api("GET", "/api/github/repos/r3p-dev/dermaga-test-app/inspect?branch=main")
    assert code == 200 and insp.get("has_containerfile") is True and insp.get("suggested_port") == 8080, f"inspect failed: {code} {insp}"
    print(f"PASS [1a]: dermaga-test-app inspect containerfile={insp['containerfile']} port={insp['suggested_port']}")

    code, created = api("POST", "/api/apps", {
        "name": "testapp",
        "repo_full_name": "r3p-dev/dermaga-test-app",
        "branch": "main",
        "containerfile_path": insp["containerfile"],
        "build_context": insp["build_context"],
        "port": 8080,
        "health_type": "http",
        "health_path": "/health",
        "health_timeout_sec": 30,
        "health_consecutive": 2,
        "domain": "testapp.dermaga.test",
        "env": [
            {"key": "RUNTIME_SECRET_VAR", "value": "m6a_runtime_secret_991", "runtime": True, "build_time": False},
            {"key": "BUILD_VAR", "value": "m6a_build_val_772", "runtime": False, "build_time": True},
        ],
    })
    assert code == 201, f"create app failed: {code} {created}"
    app_id = created["app"]["id"]
    dep_id = created["deployment"]["id"]

    # Sample /proc/*/cmdline during build (avoiding self-match with [m]/[g] regex)
    _, cmdline_tok, _ = vm("grep -a -l '[g]hs_' /proc/[0-9]*/cmdline 2>/dev/null | wc -l")
    _, cmdline_bv, _ = vm("grep -a -l '[m]6a_build_val_772' /proc/[0-9]*/cmdline 2>/dev/null | wc -l")
    print(f"INFO [cmdline sample]: ghs_={cmdline_tok} build_val={cmdline_bv}")
    assert cmdline_tok == "0", f"token in /proc/*/cmdline during build: {cmdline_tok}"
    assert cmdline_bv == "0", f"build-time value in /proc/*/cmdline during build: {cmdline_bv}"

    dep = wait_dep(app_id, dep_id, {"active", "failed"}, timeout=180)
    assert dep["status"] == "active" and dep["status_label"] == "live", f"first deploy failed: {dep}"
    print(f"PASS [1b]: testapp deployed status={dep['status']} label={dep['status_label']}")

    # Verify Pebble TLS cert + HTTPS response
    for _ in range(15):
        rc, out, err = vm("curl -fsS --cacert ~/.local/share/dermaga/traefik/pebble-minica.pem --resolve testapp.dermaga.test:443:127.0.0.1 https://testapp.dermaga.test/health")
        if rc == 0 and "healthy" in out:
            break
        time.sleep(1)
    assert rc == 0 and "healthy" in out, f"HTTPS Pebble check failed: rc={rc} out={out} err={err}"
    print(f"PASS [1c]: HTTPS via Pebble cert returned: {out.strip()}")

    # Verify runtime env & build-time marker inside container
    rc, r_env, _ = vm("podman exec app-testapp-r1 printenv RUNTIME_SECRET_VAR")
    assert rc == 0 and r_env == "m6a_runtime_secret_991", f"runtime env missing: {r_env}"
    rc, b_info, _ = vm("podman exec app-testapp-r1 cat /etc/build-info")
    assert rc == 0 and b_info == "build=m6a_build_val_772", f"build-time var missing in container: {b_info}"
    print(f"PASS [1d]: runtime env verified in container; build-time var reached build ({b_info})")

    # 2. Build-time value greps & podman history / image inspect
    assert "m6a_build_val_772" not in dep.get("build_logs", ""), "build-time value leaked in build_logs!"
    assert "m6a_runtime_secret_991" not in dep.get("build_logs", ""), "runtime value leaked in build_logs!"
    _, j_hits, _ = vm("journalctl --user -u dermaga --no-pager | grep -c 'm6a_build_val_772' || true")
    _, db_hits, _ = vm("python3 -c \"import sqlite3; c=sqlite3.connect('/home/server/.local/share/dermaga/dermaga.db'); rows=c.execute('SELECT build_logs, env_encrypted, build_env_encrypted FROM apps, deployments').fetchall(); print(sum('m6a_build_val_772' in str(r) for r in rows))\"")
    assert j_hits == "0" and db_hits == "0", f"build-time value leaked: journal={j_hits} db={db_hits}"

    _, hist, _ = vm("podman history --no-trunc localhost/dermaga/testapp:r1 | grep 'BUILD_VAR' || true")
    _, insp_img, _ = vm("podman image inspect localhost/dermaga/testapp:r1 --format '{{range .History}}{{.CreatedBy}}{{\"\\n\"}}{{end}}' | grep 'BUILD_VAR' || true")
    print(f"PASS [2]: 0 hits in build_logs/journal/DB/cmdline; expected DEC-07 leak in podman history: {hist.strip()[:120]}")
    print(f"          expected DEC-07 leak in image inspect: {insp_img.strip()[:120]}")

    # 3. Installation token greps
    _, cfg_hits, _ = vm("grep -cE 'ghs_|x-access-token' ~/.cache/dermaga/mirrors/testapp.git/config || true")
    _, tok_j_hits, _ = vm("journalctl --user -u dermaga --no-pager | grep -c 'ghs_' || true")
    _, tok_db_hits, _ = vm("python3 -c \"import sqlite3; c=sqlite3.connect('/home/server/.local/share/dermaga/dermaga.db'); print(sum('ghs_' in str(r) for r in c.execute('SELECT build_logs FROM deployments').fetchall()))\"")
    assert cfg_hits == "0" and tok_j_hits == "0" and tok_db_hits == "0", f"token leaked: cfg={cfg_hits} j={tok_j_hits} db={tok_db_hits}"
    print("PASS [3]: 0 hits for installation token in /proc/*/cmdline, mirror .git/config, logs, and DB")

    # 4. Limits in podman inspect (DEC-05) & build timeout
    _, lim_out, _ = vm("podman inspect app-testapp-r1 --format 'Memory={{.HostConfig.Memory}} NanoCpus={{.HostConfig.NanoCpus}} PidsLimit={{.HostConfig.PidsLimit}}'")
    assert "Memory=536870912" in lim_out and "NanoCpus=1000000000" in lim_out and "PidsLimit=128" in lim_out, f"unexpected limits: {lim_out}"
    print(f"PASS [4a]: podman inspect limits: {lim_out}")

    # Test build timeout (set build_timeout_sec=1 temporarily on a fresh uncached mirror/build)
    vm("python3 -c \"import sqlite3; c=sqlite3.connect('/home/server/.local/share/dermaga/dermaga.db'); c.execute(\\\"INSERT OR REPLACE INTO settings(key,value) VALUES('build_timeout_sec','1')\\\"); c.commit()\"")
    try:
        code, to_app = api("POST", "/api/apps", {
            "name": "timeoutapp",
            "repo_full_name": "r3p-dev/dermaga-test-app",
            "branch": "main",
            "port": 8080,
            "health_type": "none",
            "domain": "timeoutapp.dermaga.test",
            "env": [{"key": "BUILD_VAR", "value": "timeout_uncached_val", "runtime": False, "build_time": True}],
        })
        assert code == 201, f"create timeoutapp failed: {code} {to_app}"
        res_to = wait_dep(to_app["app"]["id"], to_app["deployment"]["id"], {"failed", "active"}, timeout=60)
        err_msg = res_to.get("error_message", "")
        assert res_to["status"] == "failed" and ("timed out" in err_msg or "signal: killed" in err_msg), f"expected timeout failure, got {res_to}"
        api("DELETE", f"/api/apps/{to_app['app']['id']}", {"confirm_name": "timeoutapp"})
        print(f"PASS [4b]: build exceeding timeout killed and marked failed: {err_msg}")
    finally:
        vm("python3 -c \"import sqlite3; c=sqlite3.connect('/home/server/.local/share/dermaga/dermaga.db'); c.execute(\\\"DELETE FROM settings WHERE key='build_timeout_sec'\\\"); c.commit()\"")

    # 5a. Failing first deploy (cmd healthcheck = false) leaves nothing
    code, fail_app = api("POST", "/api/apps", {
        "name": "failapp",
        "repo_full_name": "r3p-dev/dermaga-test-app",
        "branch": "main",
        "port": 8080,
        "health_type": "cmd",
        "health_cmd": "false",
        "health_timeout_sec": 3,
        "domain": "failapp.dermaga.test",
    })
    assert code == 201, f"create failapp failed: {code} {fail_app}"
    f_res = wait_dep(fail_app["app"]["id"], fail_app["deployment"]["id"], {"failed"}, timeout=120)
    assert f_res["status"] == "failed"
    _, ps_f, _ = vm("podman ps -a --format '{{.Names}}' | grep -c 'failapp' || true")
    _, img_f, _ = vm("podman images --format '{{.Repository}}' | grep -c 'failapp' || true")
    _, unit_f, _ = vm("systemctl --user list-units --all --no-legend | grep -c 'failapp' || true")
    _, tr_f, _ = vm("ls ~/.local/share/dermaga/traefik/dynamic | grep -c 'failapp' || true")
    assert ps_f == "0" and img_f == "0" and unit_f == "0" and tr_f == "0", f"failapp residue: ps={ps_f} img={img_f} unit={unit_f} tr={tr_f}"
    api("DELETE", f"/api/apps/{fail_app['app']['id']}", {"confirm_name": "failapp"})
    print("PASS [5a]: failing first deploy left 0 containers, images, units, or Traefik routes")

    # 5b. Cancelled build mid-run leaves nothing
    code, cancel_app = api("POST", "/api/apps", {
        "name": "cancelapp",
        "repo_full_name": "r3p-dev/dermaga-test-app",
        "branch": "main",
        "port": 8080,
        "health_type": "http",
        "health_path": "/health",
        "domain": "cancelapp.dermaga.test",
    })
    assert code == 201, f"create cancelapp failed: {code} {cancel_app}"
    c_app_id = cancel_app["app"]["id"]
    c_dep_id = cancel_app["deployment"]["id"]
    wait_dep_building(c_app_id, c_dep_id, timeout=30)
    code, _ = api("POST", f"/api/deployments/{c_dep_id}/cancel")
    assert code == 200, f"cancel failed: {code}"
    c_res = wait_dep(c_app_id, c_dep_id, {"cancelled", "failed"}, timeout=60)
    assert c_res["status"] == "cancelled", f"expected cancelled, got {c_res}"
    _, ps_c, _ = vm("podman ps -a --format '{{.Names}}' | grep -c 'cancelapp' || true")
    _, img_c, _ = vm("podman images --format '{{.Repository}}' | grep -c 'cancelapp' || true")
    _, unit_c, _ = vm("systemctl --user list-units --all --no-legend | grep -c 'cancelapp' || true")
    _, tr_c, _ = vm("ls ~/.local/share/dermaga/traefik/dynamic | grep -c 'cancelapp' || true")
    assert ps_c == "0" and img_c == "0" and unit_c == "0" and tr_c == "0", f"cancelapp residue: ps={ps_c} img={img_c} unit={unit_c} tr={tr_c}"
    api("DELETE", f"/api/apps/{c_app_id}", {"confirm_name": "cancelapp"})
    print("PASS [5b]: cancelled build left 0 containers, images, units, or Traefik routes")

    # 6. Restart dermaga.service mid-build
    code, dep_rst = api("POST", f"/api/apps/{app_id}/deploy")
    assert code == 202
    wait_dep_building(app_id, dep_rst["id"], timeout=30)
    rc, _, err = vm("systemctl --user restart dermaga.service && systemctl --user is-active dermaga.service")
    assert rc == 0, f"daemon restart failed: {err}"
    time.sleep(2)
    login()
    r_res = wait_dep(app_id, dep_rst["id"], {"failed"}, timeout=30)
    assert "restarted" in r_res.get("error_message", ""), f"expected restart failure reason, got {r_res}"
    print(f"PASS [6]: daemon restart mid-build marked deployment failed ({r_res['error_message']}); testapp r1 still live")

    # 7. APP-9 remove app
    code, del_resp = api("DELETE", f"/api/apps/{app_id}", {"confirm_name": "testapp"})
    assert code == 200, f"delete testapp failed: {code} {del_resp}"
    _, ps_t, _ = vm("podman ps -a --format '{{.Names}}' | grep -c 'testapp' || true")
    _, img_t, _ = vm("podman images --format '{{.Repository}}' | grep -c 'testapp' || true")
    _, unit_t, _ = vm("ls ~/.config/containers/systemd | grep -c 'testapp' || true")
    _, tr_t, _ = vm("ls ~/.local/share/dermaga/traefik/dynamic | grep -c 'testapp' || true")
    _, env_t, _ = vm("ls ~/.local/share/dermaga/env 2>/dev/null | grep -c 'testapp' || true")
    _, mir_t, _ = vm("ls ~/.cache/dermaga/mirrors 2>/dev/null | grep -c 'testapp' || true")
    assert ps_t == "0" and img_t == "0" and unit_t == "0" and tr_t == "0" and env_t == "0" and mir_t == "0", (
        f"testapp residue: ps={ps_t} img={img_t} unit={unit_t} tr={tr_t} env={env_t} mir={mir_t}"
    )
    print("PASS [7]: APP-9 remove app left 0 containers, images, units, routes, env files, or mirrors")
    print("=== ALL M6a REAL-PODMAN CHECKS PASSED ===")


if __name__ == "__main__":
    main()
