# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/unifi-block-sync .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/unifi-block-sync /unifi-block-sync
USER nonroot:nonroot
ENTRYPOINT ["/unifi-block-sync"]
