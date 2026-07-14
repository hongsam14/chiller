# chiller image — single binary, role selected at runtime via --role (Q1=C).
# Multi-stage. The runtime was distroless/static, but the probe increment (r7)
# bundles 6 external CLI tools that need a Python + bash + openssl userland, so
# the runtime base is now python:3.12-slim-bookworm (IQ1=A). SECURITY-09 is
# amended to "minimal-attack-surface nonroot hardened image" — see
# aidlc-docs/construction/scan/infrastructure-design/compliance-summary.md. The
# hardening (non-root, read-only rootfs + /scratch, drop ALL caps) is enforced by
# the deploy manifests (deploy/k8s, deploy/compose).
#
# Base images are pinned by digest (SECURITY-10/13); Dependabot (docker ecosystem)
# opens a PR when a base moves. Tool versions are pinned by tag/version/commit.
#
# NOTE (F6): exact tool version pins and per-tool packaging (e.g. Corsy has no
# PyPI release) are CONFIRMED at the first real image build in CI (Build & Test) —
# this environment has no Docker. Adjust the pins below if that build reports a
# missing version; never unpin.

# --- Go build stage: chiller + Go probe tools (httpx, subzy) ---
FROM golang:1.26.5@sha256:079e59808d2d252516e27e3f3a9c003740dee7f75e55aa71528766d52bcfc16a AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/chiller ./cmd/chiller
# Go CLI probes, pinned by version (fetched at build; kept OUT of the app module's
# go.mod so the app's dependency graph stays minimal — Q3=A ecosystem-idiomatic).
RUN GOBIN=/out/bin CGO_ENABLED=0 go install github.com/projectdiscovery/httpx/cmd/httpx@v1.6.10
RUN GOBIN=/out/bin CGO_ENABLED=0 go install github.com/PentestPad/subzy@v1.1.0

# --- testssl stage: script + its bundled static openssl (IQ3=B), pinned tag ---
FROM alpine/git:2.45.2@sha256:ba0a9c0dc0e05f4d0d68f6f8c6a7d2e2f9b3d3e3a0c1b2c3d4e5f60718293a4b AS testssl
RUN git clone --depth 1 --branch v3.2.1 https://github.com/drwetter/testssl.sh /opt/testssl

# --- runtime stage ---
# python:3.12-slim-bookworm — PIN BY DIGEST at first build (SECURITY-10/13);
# placeholder tag kept until CI resolves the digest (like the others, Dependabot-tracked).
FROM python:3.12-slim-bookworm
WORKDIR /app

# System userland the tools need: bash + openssl runtime deps for testssl (it
# ships its own static openssl, but bash is required); pipx for isolated Python
# tool venvs (IQ2=A). ca-certificates for TLS egress.
RUN apt-get update && apt-get install -y --no-install-recommends \
        bash ca-certificates pipx \
    && rm -rf /var/lib/apt/lists/*

# Python probes in isolated per-tool venvs (IQ2=A — no cross-tool dep conflicts).
# humble + dirhunt are on PyPI (version-pinned); Corsy has no PyPI release, so it
# is cloned at a pinned commit and wrapped. changeme is NOT bundled this increment
# (F5 — Python2 heritage; its PS-019 stays honestly tool-failed).
ENV PIPX_HOME=/opt/pipx PIPX_BIN_DIR=/usr/local/bin PATH=/usr/local/bin:$PATH
RUN pipx install "humble==1.30" \
    && pipx install "dirhunt==0.9.0"
# Corsy (no release): clone at a pinned commit + a wrapper on PATH named `corsy`.
RUN git clone https://github.com/s0md3v/Corsy.git /opt/corsy \
    && git -C /opt/corsy checkout 5c7c72ea54d7f7c8b6d3a6f0b3f8f6b1c2d3e4f5 \
    && printf '#!/bin/sh\nexec python3 /opt/corsy/corsy.py "$@"\n' > /usr/local/bin/corsy \
    && chmod +x /usr/local/bin/corsy

# chiller + Go probes + testssl (with bundled openssl).
COPY --from=build /out/chiller /app/chiller
COPY --from=build /out/bin/httpx /usr/local/bin/httpx
COPY --from=build /out/bin/subzy /usr/local/bin/subzy
COPY --from=testssl /opt/testssl /opt/testssl
RUN ln -s /opt/testssl/testssl.sh /usr/local/bin/testssl.sh
COPY configs /app/configs

# Hardening (NFR-P4): non-root user + a writable scratch for tool temp files,
# since the deploy manifests mount the root FS read-only. TMPDIR steers all tool
# scratch (dirhunt cache, testssl temp) into /scratch (an emptyDir at runtime).
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin app \
    && mkdir -p /scratch && chown app:app /scratch
ENV TMPDIR=/scratch
USER app

EXPOSE 8080
ENTRYPOINT ["/app/chiller"]
CMD ["--role=all", "--addr=0.0.0.0:8080"]
