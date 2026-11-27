# Release image, built by goreleaser's dockers_v2 from the binaries it has already built. It is not
# meant for `docker build` on its own: the build context is goreleaser's, laid out by $TARGETPLATFORM.
# The base is static and non-root (UID 65532) with CA certificates, pinned by digest and kept current
# by Renovate.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/releash /usr/bin/releash
ENTRYPOINT ["/usr/bin/releash"]
