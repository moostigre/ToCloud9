FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/testrealm-operator ./apps/testrealm-operator
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/operator ./apps/testrealm-operator

FROM alpine:3.21 AS kubectl
RUN apk add --no-cache kubectl

FROM ubuntu:22.04
RUN apt-get update && apt-get install -y --no-install-recommends bash ca-certificates curl jq default-mysql-client && rm -rf /var/lib/apt/lists/*
COPY --from=kubectl /usr/bin/kubectl /usr/local/bin/kubectl
COPY --from=build /out/operator /usr/local/bin/testrealm-operator
COPY apps/testrealm-operator/tc9-realm /usr/local/libexec/tc9-realm
COPY chart/files/worldserver.conf /opt/tc9/worldserver.conf
RUN useradd --create-home --uid 65532 --gid operator tc9operator && chmod 0555 /usr/local/libexec/tc9-realm
USER tc9operator
ENTRYPOINT ["/usr/local/bin/testrealm-operator"]
