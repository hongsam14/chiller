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
RUN GOBIN=/out/bin CGO_ENABLED=0 go install github.com/LukaSikic/subzy@v1.1.0
# testssl.sh (script + its bundled static openssl, IQ3=B) — cloned here since the
# golang image already has git; pinned by tag.
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
        bash ca-certificates pipx bsdextrautils procps dnsutils \
    && rm -rf /var/lib/apt/lists/*
# testssl.sh needs `hexdump` (bsdextrautils) and a DNS lookup tool (`dig` from
# dnsutils); procps provides process tools. All three were surfaced by the first
# in-image run (testssl aborted "You need hexdump" then "Neither dig/host/... present").

# Python probes. dirhunt is on PyPI (isolated venv via pipx, IQ2=A). humble
# (rfc-st/humble) and Corsy (s0md3v/Corsy) have NO PyPI release — they are git
# scripts, cloned + wrapped on PATH. changeme is NOT bundled this increment
# (F5 — Python2 heritage; its PS-019 stays honestly tool-failed).
# Pin humble/Corsy to a commit at first real build (#0b); default branch until then.
ENV PIPX_HOME=/opt/pipx PIPX_BIN_DIR=/usr/local/bin PATH=/usr/local/bin:$PATH
RUN pipx install "dirhunt==0.9.0"
RUN apt-get update && apt-get install -y --no-install-recommends git \
    && git clone --depth 1 https://github.com/rfc-st/humble.git /opt/humble \
    # humble's runtime deps (colorama/tldextract/defusedcsv/fpdf2/requests/xlsxwriter);
    # skip the pytest dev deps its requirements.txt also lists.
    && pip install --no-cache-dir --break-system-packages \
        colorama tldextract defusedcsv fpdf2 requests xlsxwriter \
    && printf '#!/bin/sh\nexec python3 /opt/humble/humble.py "$@"\n' > /usr/local/bin/humble \
    && git clone --depth 1 https://github.com/s0md3v/Corsy.git /opt/corsy \
    && printf '#!/bin/sh\nexec python3 /opt/corsy/corsy.py "$@"\n' > /usr/local/bin/corsy \
    && chmod +x /usr/local/bin/humble /usr/local/bin/corsy \
    && apt-get purge -y git && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*

# chiller + Go probes + testssl (with bundled openssl).
COPY --from=build /out/chiller /app/chiller
COPY --from=build /out/bin/httpx /usr/local/bin/httpx
COPY --from=build /out/bin/subzy /usr/local/bin/subzy
COPY --from=build /opt/testssl /opt/testssl
RUN ln -s /opt/testssl/testssl.sh /usr/local/bin/testssl.sh
COPY configs /app/configs

# Hardening (NFR-P4): non-root user + a writable scratch for tool temp files,
# since the deploy manifests mount the root FS read-only. TMPDIR steers all tool
# scratch (dirhunt cache, testssl temp) into /scratch (an emptyDir at runtime).
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin app \
    && mkdir -p /scratch && chown app:app /scratch
# TMPDIR steers tool scratch here; HOME=/scratch so tools that cache in $HOME
# (subzy downloads its fingerprints DB there on first run) work under a
# read-only root FS, where /home/app would not be writable (#0c).
ENV TMPDIR=/scratch HOME=/scratch
USER app

EXPOSE 8080
ENTRYPOINT ["/app/chiller"]
CMD ["--role=all", "--addr=0.0.0.0:8080"]
