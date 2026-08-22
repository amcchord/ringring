#!/usr/bin/env python3

import http.server
import pathlib
import sys
import threading
import time

import linphone


class SmokeFailure(Exception):
    pass


class ProvisioningServer(http.server.ThreadingHTTPServer):
    def __init__(self, address, document):
        super().__init__(address, ProvisioningHandler)
        self.document = document
        self.fetches = 0
        self.fetch_lock = threading.Lock()


class ProvisioningHandler(http.server.BaseHTTPRequestHandler):
    def log_message(self, _format, *args):
        return

    def do_GET(self):
        if self.path != "/linphone.xml":
            self.send_error(404)
            return
        with self.server.fetch_lock:
            self.server.fetches += 1
        self.send_response(200)
        self.send_header("Content-Type", "application/xml; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Content-Length", str(len(self.server.document)))
        self.end_headers()
        self.wfile.write(self.server.document)


def quiet_logs():
    try:
        linphone.LoggingService.get().set_log_level(linphone.LogLevel.LogLevelFatal)
    except (AttributeError, TypeError):
        pass


def run():
    provision_path = pathlib.Path("/provision/linphone.xml")
    if not provision_path.is_file():
        raise SmokeFailure("the generated provisioning document is missing")
    for relative_path in (".cache/linphone", ".config/linphone", ".local/share/linphone"):
        pathlib.Path("/state", relative_path).mkdir(parents=True, exist_ok=True)

    server = ProvisioningServer(("127.0.0.1", 8080), provision_path.read_bytes())
    server_thread = threading.Thread(target=server.serve_forever, daemon=True)
    server_thread.start()

    quiet_logs()
    core = linphone.Factory.get().create_core("/state/linphonerc", None, None)
    core.provisioning_uri = "http://127.0.0.1:8080/linphone.xml"
    if core.start() != 0:
        server.shutdown()
        raise SmokeFailure("the Linphone core could not start")

    try:
        deadline = time.monotonic() + 30
        registered = False
        while time.monotonic() < deadline:
            core.iterate()
            accounts = core.account_list
            if len(accounts) == 1 and accounts[0].state == linphone.RegistrationState.RegistrationStateOk:
                registered = True
                break
            if accounts and any(
                account.state == linphone.RegistrationState.RegistrationStateFailed
                for account in accounts
            ):
                raise SmokeFailure("the provisioned SIP account failed to register")
            time.sleep(0.02)

        if not registered:
            raise SmokeFailure(
                "the account did not register in time "
                f"(XML fetches: {server.fetches}, accounts: {len(core.account_list)}, "
                f"core state: {core.global_state.name})"
            )
        if len(core.account_list) != 1:
            raise SmokeFailure("provisioning did not create exactly one SIP account")
        if server.fetches != 1:
            raise SmokeFailure("Linphone did not fetch the provisioning document exactly once")

        pathlib.Path("/state/registered").write_text("ok\n", encoding="ascii")
        stop_path = pathlib.Path("/state/stop")
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline and not stop_path.exists():
            core.iterate()
            time.sleep(0.02)
        if not stop_path.exists():
            raise SmokeFailure("the registrar assertion did not complete in time")
    finally:
        if core.global_state == linphone.GlobalState.GlobalStateOn:
            core.stop()
        server.shutdown()
        server.server_close()

    print(
        "Linphone provisioning smoke passed: one remote config fetch, "
        "one account, and successful SIP registration."
    )


if __name__ == "__main__":
    try:
        run()
    except SmokeFailure as error:
        print(f"Linphone provisioning smoke failed: {error}.", file=sys.stderr)
        sys.exit(1)
    except Exception as error:
        print(
            "Linphone provisioning smoke failed with an unexpected "
            f"{type(error).__name__}.",
            file=sys.stderr,
        )
        sys.exit(1)
