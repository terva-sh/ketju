# The ketju release image (TKT-01M3YSVEDJ): the release binary on scratch,
# with CA certificates, running as uid 65532, serving from /data.
#
# It is built from published release binaries, not from source. Each release
# workflow downloads its forge's release, checks it against SHA256SUMS, and
# builds with that directory as the context. .github/workflows/release.yml
# pushes the result to ghcr.io/terva-sh/ketju; .forgejo/workflows/release.yml
# builds and checks it and pushes nothing (TKT-01M41WY0FE). By hand:
#
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     --build-arg VERSION=v1.0.0 --file Dockerfile DIR-HOLDING-THE-RELEASE
#
# CERTS_IMAGE is any Alpine image. The Forgejo workflow sets it to the
# instance's own registry copy, because its runners pull from there.
#
# What serve needs from the image (docs/02):
#   - CA certificates, because OIDC discovery calls the provider over HTTPS;
#   - no zone database, because the binary embeds one (time/tzdata);
#   - a writable data directory, the /data volume, owned by the uid it runs
#     as, with config.toml in it (KETJU_CONFIG);
#   - nothing else: the binary is static, with no C library to load.

ARG CERTS_IMAGE=docker.io/library/alpine:3.22

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
