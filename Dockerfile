# syntax = docker/dockerfile:experimental

FROM concourse/golang-builder AS builder
  WORKDIR /src
  COPY go.mod /src/go.mod
  COPY go.sum /src/go.sum
  RUN --mount=type=cache,target=/root/.cache/go-build go get -d ./...
  COPY . /src
  ENV CGO_ENABLED 0
  RUN go build -o /assets/prototype ./cmd/prototype

FROM moby/buildkit:v0.8.0 AS prototype
  COPY --from=builder /assets/prototype /usr/bin/
  COPY bin/setup-cgroups /usr/bin/
  ENTRYPOINT ["prototype"]

FROM prototype
