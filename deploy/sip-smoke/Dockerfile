FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce AS build

ARG SIPP_VERSION=3.7.7
ARG SIPP_SHA256=e55b15f567760e9febeef366a1ab51a5239d197a132ce931b78c826d22d31e69

RUN apk add --no-cache \
      binutils cmake curl g++ gcc libpcap-dev make ncurses-dev ninja openssl-dev zlib-dev \
    && curl --fail --show-error --silent --location \
      --output /tmp/sipp.tar.gz \
      "https://github.com/SIPp/sipp/releases/download/v${SIPP_VERSION}/sipp-${SIPP_VERSION}.tar.gz" \
    && echo "${SIPP_SHA256}  /tmp/sipp.tar.gz" | sha256sum -c \
    && install -d /src \
    && tar --extract --gzip --file /tmp/sipp.tar.gz --directory /src --strip-components=1 \
    && cmake -S /src -B /build -G Ninja \
      -DCMAKE_BUILD_TYPE=Release -DBUILD_STATIC=0 -DUSE_PCAP=1 -DUSE_SSL=1 \
    && cmake --build /build

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce
RUN apk add --no-cache libgcc libpcap libstdc++ ncurses-libs openssl
COPY --from=build /build/sipp /sipp
ENTRYPOINT ["/sipp"]
