#!/usr/bin/env python3

import http.server
import math
import pathlib
import struct
import sys
import threading
import time
import wave

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


def write_test_tone(path):
    sample_rate = 8000
    silence_frames = sample_rate
    tone_frames = sample_rate * 4
    samples = bytearray()
    for index in range(silence_frames + tone_frames):
        sample = 0
        if index >= silence_frames:
            sample = round(12000 * math.sin(2 * math.pi * 440 * index / sample_rate))
        samples.extend(struct.pack("<h", sample))
    with wave.open(str(path), "wb") as audio:
        audio.setnchannels(1)
        audio.setsampwidth(2)
        audio.setframerate(sample_rate)
        audio.writeframes(samples)


def verify_recorded_echo(path):
    if not path.is_file():
        raise SmokeFailure("Linphone did not create the received-audio recording")
    with wave.open(str(path), "rb") as audio:
        if audio.getsampwidth() != 2 or audio.getcomptype() != "NONE":
            raise SmokeFailure("the received-audio recording was not linear 16-bit WAV")
        channels = audio.getnchannels()
        sample_rate = audio.getframerate()
        frames = audio.getnframes()
        raw = audio.readframes(frames)
    if channels < 1 or sample_rate < 8000 or frames < sample_rate * 2:
        raise SmokeFailure("the received-audio recording was incomplete")

    unpacked = struct.unpack(f"<{len(raw) // 2}h", raw)
    samples = unpacked[::channels]
    window_size = sample_rate // 2
    strongest_rms = 0.0
    strongest_tone = 0.0
    for start in range(0, len(samples) - window_size + 1, window_size):
        window = samples[start : start + window_size]
        mean = sum(window) / len(window)
        centered = [sample - mean for sample in window]
        rms = math.sqrt(sum(sample * sample for sample in centered) / len(centered))
        sine = sum(
            sample * math.sin(2 * math.pi * 440 * index / sample_rate)
            for index, sample in enumerate(centered)
        )
        cosine = sum(
            sample * math.cos(2 * math.pi * 440 * index / sample_rate)
            for index, sample in enumerate(centered)
        )
        tone_amplitude = 2 * math.hypot(sine, cosine) / len(centered)
        strongest_rms = max(strongest_rms, rms)
        strongest_tone = max(strongest_tone, tone_amplitude)
    if strongest_rms < 1000 or strongest_tone < 1000:
        raise SmokeFailure(
            "the returned audio did not contain the expected test tone "
            f"(RMS: {strongest_rms:.0f}, tone: {strongest_tone:.0f})"
        )


def wait_for_marker(core, path, seconds, description):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline and not path.exists():
        core.iterate()
        time.sleep(0.02)
    if not path.exists():
        raise SmokeFailure(f"{description} did not arrive in time")


def place_party_call(core):
    destination = linphone.Factory.get().create_address("sip:102@172.31.90.20")
    if destination is None:
        raise SmokeFailure("Linphone could not parse the extension address")
    call = core.invite_address(destination)
    if call is None:
        raise SmokeFailure("Linphone could not create the extension call")
    terminal_states = {
        linphone.CallState.CallStateEnd,
        linphone.CallState.CallStateError,
        linphone.CallState.CallStateReleased,
    }
    deadline = time.monotonic() + 20
    while time.monotonic() < deadline:
        core.iterate()
        if call.state == linphone.CallState.CallStateStreamsRunning:
            break
        if call.state in terminal_states:
            raise SmokeFailure(f"the extension call ended in {call.state.name}")
        time.sleep(0.02)
    if call.state != linphone.CallState.CallStateStreamsRunning:
        raise SmokeFailure(
            "the extension call did not start media "
            f"({call.state.name}, reason: {call.reason.name})"
        )

    deadline = time.monotonic() + 6
    while time.monotonic() < deadline:
        core.iterate()
        if call.state in terminal_states:
            raise SmokeFailure(f"the extension call ended during media in {call.state.name}")
        time.sleep(0.02)

    stats = call.audio_stats
    payload = call.current_params.used_audio_payload_type
    if stats is None or stats.rtp_packet_sent < 100 or stats.rtp_packet_recv < 100:
        sent = 0 if stats is None else stats.rtp_packet_sent
        received = 0 if stats is None else stats.rtp_packet_recv
        raise SmokeFailure(
            "the extension call did not exchange enough RTP "
            f"(sent: {sent}, received: {received})"
        )
    if payload is None or payload.mime_type.upper() not in {"PCMU", "PCMA", "G722"}:
        codec = "none" if payload is None else payload.mime_type
        raise SmokeFailure(f"the extension call negotiated an unexpected codec ({codec})")

    if call.terminate() != 0:
        raise SmokeFailure("Linphone could not end the extension call")
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline and call.state not in terminal_states:
        core.iterate()
        time.sleep(0.02)
    if call.state not in terminal_states:
        raise SmokeFailure("the extension call did not end cleanly")


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
    play_path = pathlib.Path("/state/test-tone.wav")
    record_path = pathlib.Path("/state/received-audio.wav")
    write_test_tone(play_path)

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
        wait_for_marker(
            core,
            pathlib.Path("/state/call"),
            20,
            "the registrar's call authorization",
        )
        core.use_files = True
        core.play_file = str(play_path)
        core.record_file = str(record_path)
        if not core.use_files or core.play_file != str(play_path):
            raise SmokeFailure("Linphone did not accept the file-backed audio input")
        if core.record_file != str(record_path):
            raise SmokeFailure("Linphone did not accept the file-backed audio output")
        place_party_call(core)
        verify_recorded_echo(record_path)
        pathlib.Path("/state/call-complete").write_text("ok\n", encoding="ascii")

        stop_path = pathlib.Path("/state/stop")
        wait_for_marker(core, stop_path, 20, "the final registrar assertion")
    finally:
        if core.global_state == linphone.GlobalState.GlobalStateOn:
            core.stop()
        server.shutdown()
        server.server_close()

    print(
        "Linphone provisioning smoke passed: one remote config fetch, "
        "one account, successful SIP registration, an extension call, "
        "and echoed two-way audio."
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
