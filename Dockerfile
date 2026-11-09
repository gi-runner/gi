FROM golang:1.27.2 AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/gi-runner/gi/internal/cli.version=${VERSION}" -o /gi ./cmd/gi

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /gi /usr/local/bin/gi
WORKDIR /work
ENTRYPOINT ["gi"]
