FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w' -o /out/healbot ./cmd/healbot

FROM debian:bookworm-slim
RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/* \
  && useradd --uid 65532 --create-home healbot
USER 65532
COPY --from=build /out/healbot /usr/local/bin/healbot
COPY --from=build /src/config.demo.yaml /config.demo.yaml
EXPOSE 43117
WORKDIR /
ENTRYPOINT ["healbot"]
CMD ["-config", "/config.demo.yaml"]
