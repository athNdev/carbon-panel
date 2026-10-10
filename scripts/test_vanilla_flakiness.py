import time
import json
import urllib.request
import urllib.error
import sys

BASE_URL = "http://192.168.0.118:8081"
TOKEN = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjE3OTE3MjU4MTUsImlhdCI6MTc5MTYzOTQxNSwianRpIjoiNmEyMjM5ZWYtMjUzMS00NDE2LTg2ZjItM2RlMzViMWU0MGZkIiwicm9sZXMiOlsiYWRtaW4iXSwidXNlcl9pZCI6ImE3NTczMGYxLTFkMDEtNGM1NS05YTE0LWVkODA0MWRjMjI0NCIsInVzZXJuYW1lIjoiYWRtaW4ifQ.pPV0WJ_QrCGphido9QIel3cT2yOowycflYStjeighAA"
HEADERS = {
    "Content-Type": "application/json",
    "Authorization": f"Bearer {TOKEN}"
}

def log(msg):
    print(msg, flush=True)

def rpc_call(endpoint, data=None):
    if data is None:
        data = {}
    req = urllib.request.Request(
        f"{BASE_URL}{endpoint}",
        data=json.dumps(data).encode("utf-8"),
        headers=HEADERS,
        method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return json.loads(resp.read().decode("utf-8")), None
    except urllib.error.HTTPError as e:
        body = e.read().decode("utf-8")
        return None, f"HTTP {e.code}: {body}"
    except Exception as e:
        return None, str(e)

def wait_for_status(server_id, target_statuses, timeout_sec=40):
    start = time.time()
    last_status = "UNKNOWN"
    while time.time() - start < timeout_sec:
        res, err = rpc_call("/carbonpanel.v1.ServerService/GetServer", {"id": server_id})
        if not err and res:
            last_status = res.get("server", {}).get("status", "")
            if last_status in target_statuses:
                return last_status, True
        time.sleep(2)
    return last_status, False

def main():
    test_server_id = "0d5e28fb-9d51-492f-907f-6d1254459805"
    passed = 0
    failed = 0

    log("==================================================")
    log("STARTING 10 VANILLA SERVER RELIABILITY & FLAKINESS TESTS")
    log("==================================================")

    # Test 1: Start existing server test_vanilla
    log("\n[Test 1] Starting vanilla server test_vanilla (Cycle 1)...")
    res, err = rpc_call("/carbonpanel.v1.ServerService/StartServer", {"id": test_server_id})
    if err:
        log(f"FAILED Test 1: StartServer error: {err}")
        failed += 1
    else:
        log("PASSED Test 1: StartServer RPC initiated successfully")
        passed += 1

    # Test 2: Verify server reaches active state
    log("\n[Test 2] Verifying server status is STARTING or RUNNING...")
    status, ok = wait_for_status(test_server_id, ["SERVER_STATUS_STARTING", "SERVER_STATUS_RUNNING"], timeout_sec=30)
    if ok:
        log(f"PASSED Test 2: Server in active state: {status}")
        passed += 1
    else:
        log(f"FAILED Test 2: Server status is unexpected: {status}")
        failed += 1

    # Test 3: Stop server test_vanilla
    log("\n[Test 3] Stopping vanilla server test_vanilla (Cycle 1)...")
    res, err = rpc_call("/carbonpanel.v1.ServerService/StopServer", {"id": test_server_id})
    if err:
        log(f"FAILED Test 3: StopServer error: {err}")
        failed += 1
    else:
        log(f"PASSED Test 3: StopServer RPC response: {res.get('status')}")
        passed += 1

    # Test 4: Verify server reached STOPPED state
    log("\n[Test 4] Verifying server status is STOPPED...")
    status, ok = wait_for_status(test_server_id, ["SERVER_STATUS_STOPPED"], timeout_sec=30)
    if ok:
        log(f"PASSED Test 4: Server reached STOPPED state: {status}")
        passed += 1
    else:
        log(f"FAILED Test 4: Server failed to reach STOPPED state: {status}")
        failed += 1

    # Test 5: Verify GetNextAvailablePort avoids in-use ports (25566, 8081)
    log("\n[Test 5] Checking GetNextAvailablePort avoids bound in-use ports...")
    res, err = rpc_call("/carbonpanel.v1.ServerService/GetNextAvailablePort", {})
    next_port = res.get("port") if res else None
    if next_port and next_port not in [25566, 8081]:
        log(f"PASSED Test 5: Port allocator chose available port {next_port}")
        passed += 1
    else:
        log(f"FAILED Test 5: Allocated invalid or in-use port: {next_port} (err={err})")
        failed += 1

    # Test 6: Create brand-new vanilla server with MOD_LOADER_VANILLA and auto-placement
    log("\n[Test 6] Creating new vanilla server 'test_vanilla_flakiness'...")
    new_port = next_port or 25570
    create_payload = {
        "name": "test_vanilla_flakiness",
        "modLoader": "MOD_LOADER_VANILLA",
        "mcVersion": "1.20.1",
        "port": new_port,
        "memory": 2048,
        "maxPlayers": 10
    }
    create_res, err = rpc_call("/carbonpanel.v1.ServerService/CreateServer", create_payload)
    new_server_id = None
    if err:
        log(f"FAILED Test 6: CreateServer error: {err}")
        failed += 1
    else:
        new_server_id = create_res.get("server", {}).get("id")
        created_loader = create_res.get("server", {}).get("modLoader")
        assigned_node = create_res.get("server", {}).get("nodeId")
        log(f"  Created server ID: {new_server_id}, Loader: {created_loader}, Port: {new_port}, Node: {assigned_node}")
        if new_server_id and created_loader == "MOD_LOADER_VANILLA":
            log("PASSED Test 6: New vanilla server created successfully with auto-placement")
            passed += 1
        else:
            log(f"FAILED Test 6: Unexpected creation response: {create_res}")
            failed += 1

    time.sleep(2)
    # Test 7: Start newly created vanilla server
    if new_server_id:
        log(f"\n[Test 7] Starting new vanilla server {new_server_id}...")
        res, err = rpc_call("/carbonpanel.v1.ServerService/StartServer", {"id": new_server_id})
        if err:
            log(f"FAILED Test 7: StartServer error: {err}")
            failed += 1
        else:
            status, ok = wait_for_status(new_server_id, ["SERVER_STATUS_STARTING", "SERVER_STATUS_RUNNING"], timeout_sec=30)
            if ok:
                log(f"PASSED Test 7: New vanilla server container started smoothly: {status}")
                passed += 1
            else:
                log(f"FAILED Test 7: New server failed to start: {status}")
                failed += 1

        # Test 8: Stop newly created vanilla server
        log(f"\n[Test 8] Stopping new vanilla server {new_server_id}...")
        res, err = rpc_call("/carbonpanel.v1.ServerService/StopServer", {"id": new_server_id})
        if err:
            log(f"FAILED Test 8: StopServer error: {err}")
            failed += 1
        else:
            status, ok = wait_for_status(new_server_id, ["SERVER_STATUS_STOPPED"], timeout_sec=30)
            if ok:
                log(f"PASSED Test 8: New vanilla server stopped cleanly: {status}")
                passed += 1
            else:
                log(f"FAILED Test 8: Server stop timeout: {status}")
                failed += 1

        # Test 9: Delete newly created server and clean up files
        log(f"\n[Test 9] Deleting new vanilla server {new_server_id}...")
        res, err = rpc_call("/carbonpanel.v1.ServerService/DeleteServer", {"id": new_server_id, "deleteFiles": True})
        if err:
            log(f"FAILED Test 9: DeleteServer error: {err}")
            failed += 1
        else:
            log("PASSED Test 9: Deleted new vanilla server successfully")
            passed += 1
    else:
        log("Skipping Tests 7, 8, 9 due to creation failure")

    # Test 10: Final cycle on test_vanilla - Start and verify container running
    log(f"\n[Test 10] Final reliability test - Restarting test_vanilla (Cycle 2)...")
    res, err = rpc_call("/carbonpanel.v1.ServerService/StartServer", {"id": test_server_id})
    if err:
        log(f"FAILED Test 10: StartServer error: {err}")
        failed += 1
    else:
        status, ok = wait_for_status(test_server_id, ["SERVER_STATUS_STARTING", "SERVER_STATUS_RUNNING"], timeout_sec=30)
        if ok:
            log(f"PASSED Test 10: Server restarted cleanly without flakiness: {status}")
            passed += 1
        else:
            log(f"FAILED Test 10: Server restart failed: {status}")
            failed += 1

    log("\n==================================================")
    log(f"FINAL RESULT: {passed} PASSED, {failed} FAILED out of 10 tests")
    log("==================================================")
    if failed > 0:
        sys.exit(1)

if __name__ == "__main__":
    main()
