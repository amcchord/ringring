#!/usr/bin/env python3
"""Generate original RingRing WAV previews and Grandstream ringtone binaries."""

from __future__ import annotations

import argparse
import math
import struct
import wave
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Callable


SAMPLE_RATE = 8_000
GRANDSTREAM_HEADER_SIZE = 512
GRANDSTREAM_MAX_SIZE = 65_536
FIXED_TIMESTAMP = datetime(2026, 8, 23, 12, 0)


@dataclass(frozen=True)
class Ringtone:
    slot: int
    slug: str
    duration: float
    target_peak: float
    compose: Callable[[list[float]], None]


def midi(note: int) -> float:
    return 440.0 * (2.0 ** ((note - 69) / 12.0))


def add_tone(
    samples: list[float],
    start: float,
    duration: float,
    frequency: float,
    amplitude: float = 0.25,
    timbre: str = "round",
) -> None:
    first = round(start * SAMPLE_RATE)
    frames = round(duration * SAMPLE_RATE)
    attack = min(0.025, duration / 5.0)
    release = min(0.12, duration / 3.0)

    for offset in range(frames):
        index = first + offset
        if index >= len(samples):
            break
        elapsed = offset / SAMPLE_RATE
        remaining = duration - elapsed
        envelope = min(1.0, elapsed / attack, remaining / release)
        phase = 2.0 * math.pi * frequency * elapsed

        if timbre == "bell":
            color = (
                math.sin(phase)
                + 0.38 * math.sin(2.01 * phase)
                + 0.16 * math.sin(3.98 * phase)
            ) / 1.54
            envelope *= math.exp(-2.2 * elapsed / duration)
        elif timbre == "bright":
            color = (
                math.sin(phase)
                + 0.28 * math.sin(2.0 * phase)
                + 0.11 * math.sin(3.0 * phase)
            ) / 1.39
        else:
            color = math.sin(phase) + 0.12 * math.sin(2.0 * phase)
            color /= 1.12

        samples[index] += amplitude * envelope * color


def add_chord(
    samples: list[float],
    start: float,
    duration: float,
    notes: tuple[int, ...],
    amplitude: float,
    timbre: str = "round",
) -> None:
    per_note = amplitude / math.sqrt(len(notes))
    for note in notes:
        add_tone(samples, start, duration, midi(note), per_note, timbre)


def compose_double_ring(samples: list[float]) -> None:
    for pair_start in (0.10, 1.55, 3.00):
        for burst_start in (pair_start, pair_start + 0.48):
            add_tone(samples, burst_start, 0.30, 660.0, 0.28, "bright")
            add_tone(samples, burst_start, 0.30, 880.0, 0.22, "bright")
        add_tone(samples, pair_start + 0.10, 0.14, 1_320.0, 0.08, "bell")


def compose_memphis_bounce(samples: list[float]) -> None:
    melody = (
        (0.08, 72),
        (0.34, 76),
        (0.60, 79),
        (0.94, 76),
        (1.28, 81),
        (1.62, 79),
        (2.05, 72),
        (2.31, 76),
        (2.57, 79),
        (2.91, 84),
        (3.25, 81),
        (3.59, 79),
    )
    for start, note in melody:
        add_tone(samples, start, 0.28, midi(note), 0.33, "bell")
    for start in (0.08, 1.28, 2.05, 3.25):
        add_tone(samples, start, 0.18, midi(60), 0.12, "round")


def compose_confetti_call(samples: list[float]) -> None:
    for base in (0.08, 1.72, 3.36):
        for offset, note in ((0.00, 72), (0.18, 76), (0.36, 79), (0.54, 84)):
            add_tone(samples, base + offset, 0.31, midi(note), 0.27, "bright")
        add_chord(samples, base + 0.78, 0.38, (76, 79, 84), 0.30, "bell")
        add_tone(samples, base + 0.94, 0.20, midi(91), 0.08, "bell")


def compose_soft_hello(samples: list[float]) -> None:
    for base in (0.10, 1.72, 3.34):
        add_chord(samples, base, 0.78, (67, 74), 0.28, "bell")
        add_chord(samples, base + 0.48, 0.88, (69, 76), 0.25, "bell")
        add_tone(samples, base + 0.82, 0.58, midi(81), 0.08, "bell")


RINGTONES = (
    Ringtone(1, "ringring-double", 4.05, 0.63, compose_double_ring),
    Ringtone(2, "memphis-bounce", 4.15, 0.56, compose_memphis_bounce),
    Ringtone(3, "confetti-call", 4.72, 0.63, compose_confetti_call),
    Ringtone(4, "soft-hello", 4.85, 0.45, compose_soft_hello),
)


def render(ringtone: Ringtone) -> list[int]:
    samples = [0.0] * round(ringtone.duration * SAMPLE_RATE)
    ringtone.compose(samples)
    peak = max(abs(sample) for sample in samples) or 1.0
    gain = ringtone.target_peak / peak
    return [
        max(-32_768, min(32_767, round(sample * gain * 32_767)))
        for sample in samples
    ]


def write_wav(path: Path, samples: list[int]) -> None:
    with wave.open(str(path), "wb") as output:
        output.setnchannels(1)
        output.setsampwidth(2)
        output.setframerate(SAMPLE_RATE)
        output.writeframes(struct.pack(f"<{len(samples)}h", *samples))


def linear_to_mulaw(sample: int) -> int:
    magnitude = sample >> 2
    mask = 0xFF
    if magnitude < 0:
        magnitude = -magnitude
        mask = 0x7F
    magnitude = min(magnitude, 8_159) + 33

    segment_ends = (0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF, 0x1FFF)
    segment = next(
        (index for index, endpoint in enumerate(segment_ends) if magnitude <= endpoint),
        8,
    )
    if segment >= 8:
        return 0x7F ^ mask
    mantissa = (magnitude >> (segment + 1)) & 0x0F
    return ((segment << 4) | mantissa) ^ mask


def checksum_words(data: bytes) -> int:
    if len(data) % 2:
        data += b"\0"
    return sum(struct.unpack(f">{len(data) // 2}H", data))


def grandstream_binary(samples: list[int]) -> bytes:
    audio = bytes(linear_to_mulaw(sample) for sample in samples)
    if len(audio) % 2:
        audio += b"\0"

    size_words = (GRANDSTREAM_HEADER_SIZE + len(audio)) // 2
    filename = b"ring.bin".ljust(16, b"\0")
    codec = 0  # G.711 mu-law, mono, 8 kHz
    stamp = FIXED_TIMESTAMP
    version = 0x01000000

    checksum_total = 0
    checksum_total += (size_words >> 16) + (size_words & 0xFFFF)
    checksum_total += (version >> 16) + (version & 0xFFFF)
    checksum_total += stamp.year
    checksum_total += (stamp.month << 8) | stamp.day
    checksum_total += (stamp.hour << 8) | stamp.minute
    checksum_total += checksum_words(filename)
    checksum_total += codec
    checksum_total += checksum_words(audio)
    checksum = (-checksum_total) & 0xFFFF

    header = struct.pack(
        ">IHIHBBBB16sH",
        size_words,
        checksum,
        version,
        stamp.year,
        stamp.month,
        stamp.day,
        stamp.hour,
        stamp.minute,
        filename,
        codec,
    )
    header += bytes(GRANDSTREAM_HEADER_SIZE - len(header))
    result = header + audio

    if len(result) > GRANDSTREAM_MAX_SIZE:
        raise ValueError(f"Grandstream ringtone is too large: {len(result)} bytes")
    if len(result) != size_words * 2:
        raise ValueError("Grandstream ringtone word count is incorrect")
    if checksum_words(result) & 0xFFFF:
        raise ValueError("Grandstream ringtone checksum is invalid")
    return result


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True, type=Path, help="Output directory")
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    for ringtone in RINGTONES:
        samples = render(ringtone)
        wav_name = f"ring{ringtone.slot}-{ringtone.slug}.wav"
        bin_name = f"ring{ringtone.slot}.bin"
        write_wav(args.output / wav_name, samples)
        (args.output / bin_name).write_bytes(grandstream_binary(samples))


if __name__ == "__main__":
    main()
