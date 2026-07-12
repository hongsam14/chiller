# chiller image — single binary, role selected at runtime via --role (Q1=C).
# Multi-stage; distroless nonroot runtime (SECURITY-09). Base images are pinned by
# digest in production (SECURITY-10/13) — the tags below are placeholders to be
# replaced with digests at release build time (no `latest`).

# --- build stage ---
FROM golang:1.25 AS build
WORKDIR /src
# module cache layer
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off for a static binary; trim paths for reproducibility.
RUN CGO_ENABLED=0 go build -trimpath -o /out/chiller ./cmd/chiller

# --- runtime stage ---
# NOTE: pin by digest in production, e.g. gcr.io/distroless/static-debian12@sha256:<digest>
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
# configs are read-only defaults; secrets/URIs come from env/mounts (SECURITY-12).
COPY --from=build /out/chiller /app/chiller
COPY configs /app/configs
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/chiller"]
CMD ["--role=all", "--addr=0.0.0.0:8080"]
