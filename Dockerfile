# The ketju release image (TKT-01M3YSVEDJ): the release binary on scratch,
# with CA certificates, running as uid 65532, serving from /data.
#
# It is built from published release binaries, not from source. The release
# workflow (.forgejo/workflows/release.yml) downloads a release, checks it
# against SHA256SUMS, and builds each platform with that directory as the
# context:
#
#   buildah build --platform linux/amd64 --build-arg VERSION=v1.0 \
#     --file Dockerfile DIR-HOLDING-THE-RELEASE
#
# docker build takes the same arguments. Outside this forge, set
# CERTS_IMAGE to any Alpine image, such as docker.io/library/alpine:3.22.
#
# What serve needs from the image (docs/02):
#   - CA certificates, because OIDC discovery calls the provider over HTTPS;
#   - no zone database, because the binary embeds one (time/tzdata);
#   - a writable data directory, the /data volume, owned by the uid it runs
#     as, with config.toml in it (KETJU_CONFIG);
#   - nothing else: the binary is static, with no C library to load.

ARG CERTS_IMAGE=container.local.sothr.com/library/golang:1.27-alpine

# Runs on the build platform, so building the other platform needs no
# emulator. It only lays out files.
FROM --platform=$BUILDPLATFORM ${CERTS_IMAGE} AS files
RUN test -s /etc/ssl/certs/ca-certificates.crt \
 && mkdir -p /rootfs/data /rootfs/tmp \
 && chmod 0750 /rootfs/data \
 && chmod 1777 /rootfs/tmp

FROM scratch
ARG VERSION
ARG TARGETARCH
COPY --from=files /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=files /rootfs/tmp /tmp
COPY --from=files --chown=65532:65532 /rootfs/data /data
COPY ketju_${VERSION}_linux_${TARGETARCH} /ketju
USER 65532:65532
ENV KETJU_CONFIG=/data/config.toml
WORKDIR /data
VOLUME /data
EXPOSE 8080
LABEL org.opencontainers.image.title="ketju" \
      org.opencontainers.image.description="A small-community message board, as one static binary" \
      org.opencontainers.image.source="https://github.com/terva-sh/ketju" \
      org.opencontainers.image.version="${VERSION}"
ENTRYPOINT ["/ketju"]
CMD ["serve"]
