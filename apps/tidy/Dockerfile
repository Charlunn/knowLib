# syntax=docker/dockerfile:1.6
FROM golang:1.22-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/tidy ./cmd/tidy

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/tidy /tidy
ENV LISTEN_ADDR=:8081
EXPOSE 8081
USER nonroot:nonroot
ENTRYPOINT ["/tidy"]
