#!/usr/bin/env python3
"""Prepare RingRing wallpaper masters for the Grandstream WP826."""

from __future__ import annotations

import argparse
from pathlib import Path

from PIL import Image


TARGET_SIZE = (240, 320)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--day", required=True, type=Path, help="Day wallpaper master PNG")
    parser.add_argument(
        "--twilight", required=True, type=Path, help="Twilight wallpaper master PNG"
    )
    parser.add_argument("--party", required=True, type=Path, help="Party wallpaper master PNG")
    parser.add_argument("--output", required=True, type=Path, help="Output directory")
    return parser.parse_args()


def resize_cover(source: Path, destination: Path) -> None:
    with Image.open(source) as image:
        image = image.convert("RGB")
        source_ratio = image.width / image.height
        target_ratio = TARGET_SIZE[0] / TARGET_SIZE[1]

        if source_ratio > target_ratio:
            crop_width = round(image.height * target_ratio)
            left = (image.width - crop_width) // 2
            image = image.crop((left, 0, left + crop_width, image.height))
        elif source_ratio < target_ratio:
            crop_height = round(image.width / target_ratio)
            top = (image.height - crop_height) // 2
            image = image.crop((0, top, image.width, top + crop_height))

        image = image.resize(TARGET_SIZE, Image.Resampling.LANCZOS)
        image.save(destination, format="PNG", optimize=True)


def main() -> None:
    args = parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    variants = {
        "ringring-memphis-day.png": args.day,
        "ringring-memphis-twilight.png": args.twilight,
        "ringring-memphis-party.png": args.party,
    }
    for filename, source in variants.items():
        resize_cover(source, args.output / filename)


if __name__ == "__main__":
    main()
