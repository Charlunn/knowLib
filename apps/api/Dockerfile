# syntax=docker/dockerfile:1.6
FROM golang:1.22-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# Static build so we can run on distroless.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/api /api
# packages/skill-bundle is mounted in compose; default path here matches the
# volume so the substitution-and-zip handler can find templates.
ENV API_STATE_PATH=/state/api.db \
    SKILL_BUNDLE_DIR=/skill-bundle \
    LISTEN_ADDR=:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/api"]
