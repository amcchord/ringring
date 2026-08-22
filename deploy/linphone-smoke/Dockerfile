FROM ubuntu:24.04@sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517

ARG LINPHONE_VERSION=5.5.3.post5+git.55b84bc556
ARG LINPHONE_WHEEL_SHA256=f67cd3ddd8694b14d9f3493032f33c5187710f2583ebaae43d71cd7f442e027d

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      ca-certificates curl libmysqlclient21 libpython3.12 libudev1 python3-minimal \
    && rm -rf /var/lib/apt/lists/* \
    && wheel="linphone-${LINPHONE_VERSION}-cp312-cp312-linux_x86_64.whl" \
    && curl --fail --show-error --silent --location \
      --output "/tmp/${wheel}" \
      "https://download.linphone.org/releases/linphone-python/5.5/linux-x86_64/${wheel}" \
    && echo "${LINPHONE_WHEEL_SHA256}  /tmp/${wheel}" | sha256sum --check \
    && install -d /usr/local/lib/python3.12/dist-packages \
    && python3 -m zipfile -e "/tmp/${wheel}" /usr/local/lib/python3.12/dist-packages \
    && rm "/tmp/${wheel}" \
    && groupadd --gid 10001 ringring \
    && useradd --uid 10003 --gid 10001 --home-dir /state --shell /usr/sbin/nologin linphone

COPY linphone-smoke.py /usr/local/bin/ringring-linphone-smoke
RUN chmod 0755 /usr/local/bin/ringring-linphone-smoke

USER 10003:10001
WORKDIR /state
ENTRYPOINT ["python3", "/usr/local/bin/ringring-linphone-smoke"]
