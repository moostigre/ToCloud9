FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY apps/testrealm-provisioner ./apps/testrealm-provisioner
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/testrealm-provisioner ./apps/testrealm-provisioner

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/testrealm-provisioner /testrealm-provisioner
USER 65532:65532
ENTRYPOINT ["/testrealm-provisioner"]
