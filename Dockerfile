# hi-directory container. Build: docker build -t hi-directory .
# Railway and similar hosts set $PORT and terminate HTTPS in front of it.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
# Railway passes the commit as RAILWAY_GIT_COMMIT_SHA; other builders can set VERSION.
ARG RAILWAY_GIT_COMMIT_SHA=dev
ARG VERSION=${RAILWAY_GIT_COMMIT_SHA}
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/hi-directory ./cmd/hi-directory

FROM scratch
COPY --from=build /out/hi-directory /hi-directory
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/hi-directory"]
