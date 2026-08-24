#!/usr/bin/env python3
"""Render a secret-bearing, per-device WP826 XML file for GDMS By CFG."""

from __future__ import annotations

import argparse
import getpass
import os
import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path
from urllib.parse import urlparse


WALLPAPERS = {
    "day": "ringring-memphis-day.png",
    "twilight": "ringring-memphis-twilight.png",
    "party": "ringring-memphis-party.png",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Create a MAC-named WP826 configuration for GDMS By CFG."
    )
    parser.add_argument("--mac", required=True, help="Twelve-digit handset MAC address")
    parser.add_argument("--sip-host", required=True, help="RingRing SIP hostname")
    parser.add_argument("--sip-user", required=True, help="RingRing SIP username/auth ID")
    parser.add_argument("--extension", required=True, help="RingRing party extension")
    parser.add_argument(
        "--asset-base-url",
        required=True,
        help="HTTPS base URL ending before /wallpapers and /ringtones",
    )
    parser.add_argument(
        "--wallpaper", choices=tuple(WALLPAPERS), default="day", help="Wallpaper variant"
    )
    parser.add_argument(
        "--ringtone", type=int, choices=range(1, 5), default=1, help="Ringtone slot"
    )
    parser.add_argument("--output-dir", required=True, type=Path)
    return parser.parse_args()


def normalized_mac(value: str) -> str:
    mac = re.sub(r"[^0-9A-Fa-f]", "", value).lower()
    if len(mac) != 12:
        raise ValueError("MAC address must contain exactly 12 hexadecimal digits")
    return mac


def validate_args(args: argparse.Namespace) -> None:
    if not re.fullmatch(r"[A-Za-z0-9.-]+", args.sip_host):
        raise ValueError("SIP host must be a DNS name or IP address without a scheme or port")
    if not re.fullmatch(r"[A-Za-z0-9_.!~*'()-]+", args.sip_user):
        raise ValueError("SIP user contains unsupported characters")
    if not re.fullmatch(r"[0-9*#]+", args.extension):
        raise ValueError("extension must contain only dialable digits, * or #")
    asset_url = urlparse(args.asset_base_url)
    if asset_url.scheme != "https" or not asset_url.netloc:
        raise ValueError("asset base URL must be an absolute HTTPS URL")
    if asset_url.query or asset_url.fragment:
        raise ValueError("asset base URL cannot include a query string or fragment")


def add_value(config: ET.Element, pvalue: str, value: str | int) -> None:
    ET.SubElement(config, pvalue).text = str(value)


def render(args: argparse.Namespace, mac: str, password: str) -> ET.ElementTree:
    base_url = args.asset_base_url.rstrip("/")
    parsed_asset_url = urlparse(base_url)
    firmware_path = parsed_asset_url.netloc + parsed_asset_url.path
    phonebook_path = (
        parsed_asset_url.netloc + "/api/v1/phone/grandstream-phonebook.xml"
    )
    root = ET.Element("gs_provision", {"version": "1"})
    ET.SubElement(root, "mac").text = mac
    config = ET.SubElement(root, "config", {"version": "1"})

    values: tuple[tuple[str, str | int], ...] = (
        ("P271", 1),
        ("P270", f"RingRing {args.extension}"),
        ("P47", f"{args.sip_host}:5061"),
        ("P35", args.sip_user),
        ("P36", args.sip_user),
        ("P34", password),
        ("P3", f"RingRing {args.extension}"),
        ("P32", 5),
        ("P130", 2),
        ("P2329", 1),
        ("P2311", 1),
        ("P2367", 1),
        ("P57", 0),
        ("P2301", 0),
        ("P2302", 1),
        ("P2303", 0),
        ("P2916", 1),
        ("P2917", f"{base_url}/wallpapers/{WALLPAPERS[args.wallpaper]}"),
        ("P6767", 2),
        ("P192", f"{firmware_path}/ringtones"),
        ("P8509", 4),
        ("P104", args.ringtone),
        ("P2939", 1),
        ("P8348", "Custom-Contacts,Custom-History,Custom-Menu"),
        ("P330", 3),
        ("P331", phonebook_path),
        ("P332", 5),
        ("P6713", args.sip_user),
        ("P6714", password),
        ("P8463", 1),
        ("P22030", 1),
        ("P194", 0),
    )
    for pvalue, value in values:
        add_value(config, pvalue, value)

    ET.indent(root, space="  ")
    return ET.ElementTree(root)


def main() -> None:
    args = parse_args()
    try:
        mac = normalized_mac(args.mac)
        validate_args(args)
    except ValueError as error:
        raise SystemExit(str(error)) from error

    password = getpass.getpass("SIP authentication password: ")
    if not password:
        raise SystemExit("SIP authentication password cannot be empty")

    args.output_dir.mkdir(parents=True, exist_ok=True)
    destination = args.output_dir / f"{mac}.xml"
    if destination.exists():
        raise SystemExit(f"refusing to overwrite existing file: {destination}")

    descriptor = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, "wb") as output:
        render(args, mac, password).write(output, encoding="utf-8", xml_declaration=True)
    print(destination)
    print(
        "This file contains a live SIP password. Upload it to GDMS, then remove the local copy securely.",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
