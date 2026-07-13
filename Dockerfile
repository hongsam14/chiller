# chiller image — single binary, role selected at runtime via --role (Q1=C).
# Multi-stage; distroless nonroot runtime (SECURITY-09).
#
# Both base images are pinned by digest (SECURITY-10/13): a tag can be moved to a
# different image, a digest cannot. Dependabot (.github/dependabot.yml) opens a PR
# when a base image moves, so pinning does not decay into an unpatched base.

# --- build stage ---
FROM golang:1.26.5@sha256:079e59808d2d252516e27e3f3a9c003740dee7f75e55aa71528766d52bcfc16a AS build
WORKDIR /src
# module cache layer
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off for a static binary; trim paths for reproducibility.
RUN CGO_ENABLED=0 go build -trimpath -o /out/chiller ./cmd/chiller

# --- runtime stage ---
FROM gcr.io/distroless/static-debian12:nonroot@sha256:b7bb25d9f7c31d2bdd1982feb4dafcaf137703c7075dbe2febb41c24212b946f
WORKDIR /app
# configs are read-only defaults; secrets/URIs come from env/mounts (SECURITY-12).
COPY --from=build /out/chiller /app/chiller
COPY configs /app/configs
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/chiller"]
CMD ["--role=all", "--addr=0.0.0.0:8080"]
