FROM debian:13-slim AS build

ARG ASTERISK_VERSION=22.10.1

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      build-essential ca-certificates curl libedit-dev libjansson-dev libncurses-dev \
      libsqlite3-dev libssl-dev libxml2-dev pkg-config uuid-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build
RUN curl --fail --show-error --silent --location \
      --remote-name "https://downloads.asterisk.org/pub/telephony/asterisk/asterisk-${ASTERISK_VERSION}.tar.gz" \
    && curl --fail --show-error --silent --location \
      --remote-name "https://downloads.asterisk.org/pub/telephony/asterisk/asterisk-${ASTERISK_VERSION}.sha256" \
    && sha256sum --check "asterisk-${ASTERISK_VERSION}.sha256" \
    && tar --extract --gzip --file "asterisk-${ASTERISK_VERSION}.tar.gz" --strip-components=1 \
    && ./configure --with-jansson-bundled \
    && make menuselect.makeopts \
    && menuselect/menuselect --disable BUILD_NATIVE menuselect.makeopts \
    && make -j"$(nproc)" \
    && make DESTDIR=/stage install

FROM debian:13-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      ca-certificates libedit2 libjansson4 libsqlite3-0 libssl3t64 libuuid1 libxml2 mpg123 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 ringring \
    && useradd --uid 10002 --gid 10001 --home-dir /var/lib/asterisk --shell /usr/sbin/nologin asterisk \
    && install -d -o 10002 -g 10001 /var/lib/asterisk /var/log/asterisk /var/run/asterisk /var/spool/asterisk /etc/asterisk

# Debian's /var/run is a symlink to /run. Copy only the installed Asterisk
# payload so Docker does not try to replace that symlink with DESTDIR's
# directory. Runtime state directories are created above and mounted as needed.
COPY --from=build /stage/usr/ /usr/
COPY --from=build /stage/var/lib/asterisk/ /var/lib/asterisk/
COPY config/ /etc/asterisk/
COPY entrypoint.sh /usr/local/bin/ringring-asterisk-entrypoint
RUN chmod 0755 /usr/local/bin/ringring-asterisk-entrypoint

EXPOSE 5060/udp 5038/tcp 10000-10199/udp
ENTRYPOINT ["/usr/local/bin/ringring-asterisk-entrypoint"]
