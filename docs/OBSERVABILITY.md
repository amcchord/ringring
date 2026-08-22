# Privacy-preserving observability

RingRing exposes a small Prometheus-compatible metrics document on the app container's loopback listener, `127.0.0.1:9090`. The reference Compose file neither exposes nor publishes that port, Caddy does not route to it, and the public web handler intentionally returns `404` for `/metrics`. `ringringctl install`, `upgrade`, and `doctor` require the private metrics health gauges to pass and reject a public metrics response other than `404`.

Inspect the reference deployment from the host without opening a port:

```sh
cd /opt/ringring
docker compose exec -T app curl --fail --silent http://127.0.0.1:9090/metrics
```

## Privacy budget

The metrics registry is memory-only and resets when the app restarts. RingRing itself does not persist or remote-write a scrape. Labels come from fixed code allowlists and never include a party, host, member, device, SIP username, extension, invitation/provisioning token, address, user-agent, weather place, station, model prompt, caller input, transcript, or error text.

HTTP activity is reduced to one of `health`, `static`, `authentication`, `host`, `invitation`, `provisioning`, `public`, or `other`, plus `GET`/`POST`/`HEAD`/`OTHER` and a status class such as `2xx`. Voice operations use only the bounded service names `weather`, `ai_authorize`, `ai_bridge`, and `extension`, with a small result vocabulary. There is no per-call series, call start timestamp, destination, caller identity, call duration, audio, or transcript.

The current aggregate metrics are:

- `ringring_process_start_time_seconds`
- `ringring_database_up`
- `ringring_asterisk_ami_up`
- `ringring_sip_contacts{state=...}`
- `ringring_http_requests_in_flight`
- `ringring_http_requests_total{surface,method,status_class}`
- `ringring_http_request_duration_seconds{surface,method,status_class}`
- `ringring_telephony_reconciliations_total{result}`
- `ringring_voice_service_requests_total{service,result}`
- `ringring_ai_calls_active`

The SIP contact gauge is computed during the scrape. The app asks private AMI for contact state, immediately reduces it to four aggregate counts, and discards endpoint names. It never receives or exports contact URIs, source addresses, ports, user agents, or call IDs through this path.

## Logs and retention

Application request logs use route templates such as `/parties/{partyID}` rather than actual paths. Application subsystem logs omit party, member, device, SIP, extension, token, origin, and panic values. Errors describe the failed operation but must not wrap secrets or caller content.

Asterisk's dedicated security log is a separate abuse-control boundary: authentication failures necessarily include a source address so Fail2Ban can block it. It is not a call-content log, but operators should restrict it to root/the Asterisk group, rotate it, and retain it only as long as needed for abuse response. RingRing does not install a third-party metrics collector, dashboard, analytics script, remote-write destination, or retention policy.

If an operator adds Prometheus or another scraper, keep the loopback boundary and collect through a deliberately designed host-side mechanism; the production app and `ringringctl` reject a non-loopback `METRICS_ADDR`. Require authentication at every human-facing dashboard, avoid service discovery that publishes container ports, and choose the shortest useful retention. Do not add raw Asterisk events, contact details, SIP messages, HTTP paths, query strings, cookies, request bodies, AudioSocket frames, Realtime events, audio, or transcripts as labels or logs.

## Suggested alerts

- `ringring_database_up != 1` or `ringring_asterisk_ami_up != 1` for two consecutive scrapes.
- Any increase in `ringring_telephony_reconciliations_total{result="error"}`.
- A sustained increase in `ringring_http_requests_total{status_class="5xx"}`.
- `ringring_ai_calls_active` above the configured concurrency limit or unchanged beyond the configured call duration plus a small shutdown margin.

These alerts identify subsystem failure without identifying which family, phone, or caller was active. A host-triggered setup ring appears only as the fixed host HTTP route/status surface; RingRing adds no device, SIP username, extension, or call label. Physical call quality and background ringing still require the host-confirmed checklist and real-device testing; aggregate metrics do not prove them.

Creating an additional member phone uses the fixed host device-creation route surface. Metrics and application logs do not include its party, member, device label, generated SIP username, extension, provisioning token, or fan-out size.

Canceling unused invitations uses one fixed host route/status surface. Neither the active count nor any party, invitation ID, token hash, bearer link, expiry, or number of removed rows is logged or used as a metric label.
