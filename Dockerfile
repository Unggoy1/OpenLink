# hi-directory container. Build: docker build -t hi-directory .
# Railway and similar hosts set $PORT and terminate HTTPS in front of it.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/hi-directory ./cmd/hi-directory

FROM scratch
COPY --from=build /out/hi-directory /hi-directory
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/hi-directory"]
