FROM debian:13-slim AS fetch

ARG SIPP_VERSION=3.7.7
ARG SIPP_SHA256=8e8ecdbe923bf608c844038adfa35c8595400c4629d629f00d51539ac24cdfef

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && curl --fail --show-error --silent --location \
      --output /sipp "https://github.com/SIPp/sipp/releases/download/v${SIPP_VERSION}/sipp" \
    && echo "${SIPP_SHA256}  /sipp" | sha256sum --check \
    && chmod 0755 /sipp

FROM scratch
COPY --from=fetch /sipp /sipp
ENTRYPOINT ["/sipp"]
