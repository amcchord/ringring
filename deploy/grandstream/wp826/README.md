# RingRing theme for Grandstream WP826

This bundle gives the WP826 a RingRing-flavored idle screen, four original ringtones, friendlier idle softkeys, an auto-updating private phonebook, and an optional one-file SIP setup. The phone does not expose a replaceable system skin, so its built-in icons, menus, fonts, call screens, and boot animation remain Grandstream's.

For one or a few household phones, the recommended arrangement is:

- RingRing generates a one-time `ringring-wp826.xml` from the new phone's setup screen.
- The owner uploads that file directly through the WP826 web interface and deletes it afterward.
- RingRing serves the public wallpaper and ringtone files over its existing HTTPS origin.

For a larger fleet, GDMS remains useful:

- GDMS owns the handset, shared model template, and deliberate firmware updates.
- RingRing serves the public wallpaper and ringtone files over its existing HTTPS origin.
- GDMS SIP Account owns each handset's individual SIP secret. A MAC-specific XML is available for one-file setup when that is more convenient.

## Included assets

The handset-ready wallpapers are 240×320 RGB PNG files, each under 500 KB:

- `wallpapers/ringring-memphis-day.png`: warm cream and the clearest default.
- `wallpapers/ringring-memphis-twilight.png`: saturated RingRing purple.
- `wallpapers/ringring-memphis-party.png`: high-energy yellow.

The original, sample-free ringtone set contains an auditionable WAV and a Grandstream `ringN.bin` for each slot:

1. `ring1-ringring-double.wav` / `ring1.bin`: a bright double ring.
2. `ring2-memphis-bounce.wav` / `ring2.bin`: bouncy pitched percussion.
3. `ring3-confetti-call.wav` / `ring3.bin`: an ascending confetti flourish.
4. `ring4-soft-hello.wav` / `ring4.bin`: a quieter two-chime greeting.

Each binary is mono 8 kHz G.711 μ-law, below Grandstream's 64 KB handset limit, and contains the vendor checksum/header used by its ringtone loader. The same final PNG and BIN files are embedded below `web/static/wp826/` for deployment with the RingRing binary.

## Deploy the assets

Deploy this RingRing revision normally. No extra Caddy route or volume is required: the existing `/static/` handler embeds the WP826 directory in the application binary.

After the new application is live, verify these URLs, substituting the deployment hostname:

```sh
curl -fsSI https://ringring.example/static/wp826/wallpapers/ringring-memphis-day.png
curl -fsSI https://ringring.example/static/wp826/ringtones/ring1.bin
curl -fsSI https://ringring.example/static/wp826/ringtones/ring4.bin
```

All three requests should return `200`. These files contain no credential or personal data and are intentionally public. If the RingRing application cannot serve them, host the same directory on another stable HTTPS origin. Keep the ringtone basenames exactly `ring1.bin` through `ring4.bin`.

## Fastest setup: download from RingRing

On the one-time setup screen for a new or rotated phone, choose **Download WP826 setup file**. The attachment configures Account 1 with that device's RingRing SIP credential, verified TLS on port 5061, a five-minute registration interval, PCMU first, RFC2833 DTMF, the day wallpaper, all four ringtones, Contacts/History/Menu idle keys, and a five-minute private-phonebook refresh.

RingRing owns the handset's local Contacts list in this mode. Each refresh replaces it with other active phones in this party plus the `*` services currently enabled for this extension, so a revoked phone or disabled service disappears without another config upload. The endpoint uses the same device username and password over HTTPS; it reveals no credential, party/host metadata, presence, call history, or other party. Do not keep unrelated manual contacts on a dedicated RingRing WP826 because the managed refresh may remove them.

The file is deliberately partial. It does not change Wi-Fi, addressing, administrator access, or Accounts 2–6, and it contains no party, member, host, or device label. It does contain the live Account 1 SIP password and consumes the same one-time token as the RingRing app, Linphone, and phone API links. Upload it directly to the handset and delete the local copy afterward; if it is lost or exposed, rotate that phone in RingRing instead of trying to retrieve the old password.

1. Connect the WP826 to Wi-Fi and find its IP address.
2. Open that address from a browser on the same network and sign in as the phone administrator.
3. Open **Maintenance → Upgrade and Provisioning → Config File**.
4. Beside **Upload Device Configuration**, choose **Upload** and select `ringring-wp826.xml`. Do not use **Restore from Backup Package**; `.uf` backups are device-specific and may contain unrelated private state.
5. Let the phone apply the XML, then reboot once so it fetches `ring1.bin` through `ring4.bin` and the private address book.
6. Open **Contacts** and confirm the other party phones and enabled `*` lines appear. The list refreshes automatically every five minutes.
7. Confirm Account 1 says **Registered**, dial `*10`, and test an incoming and outgoing same-party call.

The direct upload path and wallpaper alias were validated on a physical WP826 running firmware 1.0.1.61. The server-side renderer is tested against the alias names published by that handset. Firmware changes can alter a vendor template, so repeat the real-phone verification after a major Grandstream update.

## Fleet setup: create the GDMS model template

1. Add the WP826 to GDMS using its MAC address and serial number, then assign it to the intended site.
2. Open **Device Template → By Model → Add Model Template**. Select **WP826**, give it a name such as `RingRing WP826`, and associate the relevant site.
3. Open **Set Parameters**, switch to the text editor, and start from `gdms/wp826-ringring-theme.pvalues.example`.
4. Replace `YOUR_RINGRING_HOST` with the public RingRing HTTPS hostname. Paste the P-value lines and save.
5. Choose **Apply All** or **Provision to Selected Devices** for existing handsets. A newly associated handset receives the model template automatically; editing a template does not automatically repush it to existing devices.
6. Reboot once after the push. Grandstream loads new resource files after restart.

The default theme selects the day wallpaper, `ring1.bin`, and the idle softkeys **Contacts · History · Menu**. To use another visual, change `P2917` to the twilight or party PNG URL. To use another tune, change `P104` to `2`, `3`, or `4`. The shared model template deliberately omits phonebook credentials; add those in the per-device account or use the MAC-specific renderer below.

GDMS can also upload the PNG under **Resources → Other Resources** and select it in the WP826 model editor. Keeping the HTTPS URL in the template is easier to version and roll out from RingRing. The current GDMS guide limits its direct custom-ringtone resource picker to GXP/DP models, so this bundle uses the WP826's firmware-resource path and `P8509` to fetch `ring1.bin` through `ring4.bin` instead.

## Add the SIP account

### Recommended: GDMS SIP Account

Keep per-phone credentials out of the shared theme:

1. Open the organization's **SIP Account** area and add an account.
2. Copy the values from the member's RingRing setup page:
   - **Account Active:** Yes
   - **Account Name / Name:** `RingRing EXTENSION`
   - **SIP Server:** `YOUR_RINGRING_HOST:5061`
   - **SIP User ID:** the generated RingRing SIP username
   - **SIP Authentication ID:** the same generated username
   - **Password:** the generated SIP password
3. Assign the account to the WP826 as **Account 1**.
4. In its account parameters select **TLS/TCP**, the **sips** URI scheme, a five-minute registration expiration, PCMU first, RTP/RFC2833 DTMF, and certificate-chain/domain validation.
5. In the device's **Phonebook → Phonebook Management** settings, enable phonebook download over HTTPS with:
   - **Server Path:** `YOUR_RINGRING_HOST/api/v1/phone/grandstream-phonebook.xml`
   - **Download Interval:** `5` minutes
   - **HTTP/HTTPS Username:** the generated RingRing SIP username
   - **HTTP/HTTPS Password:** the same generated SIP password
   - **Remove Manually-edited Entries on Download:** Yes

This keeps the theme reusable and makes password rotation a single GDMS account change.

### Optional: one MAC-specific XML

The renderer writes the SIP account, theme, and authenticated five-minute phonebook refresh into a GDMS **By CFG** file. It prompts for the password so the secret does not appear in shell history and creates the output with mode `0600`.

Create a temporary directory outside the checkout, then run:

```sh
wp826_cfg_dir="$(mktemp -d)"
python3 deploy/grandstream/wp826/tools/render_gdms_device_xml.py \
  --mac '00:0b:82:ab:cd:ef' \
  --sip-host ringring.example \
  --sip-user REPLACE_WITH_RINGRING_SIP_USERNAME \
  --extension 101 \
  --asset-base-url https://ringring.example/static/wp826 \
  --wallpaper day \
  --ringtone 1 \
  --output-dir "$wp826_cfg_dir"
```

In GDMS open **Device Template → By CFG → Import Configuration File** and upload the resulting lowercase, 12-digit MAC filename, for example `000b82abcdef.xml`. Push it to the device. GDMS requires this exact filename format and XML for By CFG; direct HTTP provisioning instead uses `cfg000b82abcdef.xml`.

The XML contains a live SIP password. Never commit it, email it, attach it to a ticket, or retain it in a shared download directory. Remove the local temporary copy after GDMS accepts it. Rotate the RingRing device credential if the file is ever exposed.

## Verify on a handset

1. Confirm the idle wallpaper, softkey order, and chosen ringtone after restart.
2. Confirm Account 1 reports **Registered** over TLS.
3. Open Contacts and confirm other active party phones plus enabled `*` services appear. Revoke a disposable phone or toggle a service, wait five minutes, and confirm the stale entry disappears.
4. Dial `*10` and verify audio in both directions.
5. Call another member in the same party by extension and test incoming ringing.
6. Confirm an extension in another party cannot be reached.

The WP826 remains an unverified physical-device entry in RingRing's compatibility matrix until these checks pass on real hardware. Keep UDP disabled for this account unless TLS registration is shown to fail on the handset firmware and the compatibility fallback is deliberately accepted.

## Roll back the theme

Apply these values through the same model template, then push and reboot:

```text
P2916 = 0
P104 = 0
P2939 = 0
P8348 = Custom-Menu,Custom-History
P8509 = 0
P6767 = 1
P192 = fm.grandstream.com/gs
P330 = 0
```

This restores the default wallpaper, system ringtone, standard idle layout, Grandstream firmware-resource path, and disables managed phonebook downloads. It does not remove or rotate the SIP account.

## Rebuild the generated assets

Ringtones are deterministic and contain no recorded samples:

```sh
python3 deploy/grandstream/wp826/tools/generate_ringtones.py \
  --output deploy/grandstream/wp826/ringtones
```

Wallpaper masters are generated from the prompts in `ARTWORK.md`, then reduced to the handset size with Pillow:

```sh
python3 deploy/grandstream/wp826/tools/prepare_wallpapers.py \
  --day /path/to/day-master.png \
  --twilight /path/to/twilight-master.png \
  --party /path/to/party-master.png \
  --output deploy/grandstream/wp826/wallpapers
```

When rebuilding either set, copy the final PNG or BIN files to the matching `web/static/wp826/` directory before deploying.

## Grandstream references

- [WP8x6 Administration Guide](https://documentation.grandstream.com/knowledge-base/wp8x6-administration-guide/)
- [GDMS Unified Communications User Guide](https://documentation.grandstream.com/knowledge-base/gdms-user-guide/)
- [SIP Device Provisioning Guide](https://documentation.grandstream.com/knowledge-base/sip-device-provisioning-guide/)
- [Grandstream XML Phonebook Guide](https://www.grandstream.com/hubfs/Product_Documentation/WP820_XML_phonebook_guide.pdf)
- [Grandstream configuration templates](https://www.grandstream.com/support/tools)
- [Grandstream firmware](https://www.grandstream.com/support/firmware)
