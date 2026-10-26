FROM golang:1.27.3-alpine AS base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

FROM base AS test
RUN apk add --no-cache gcc musl-dev
CMD ["go", "test", "-race", "-tags=integration", "-count=1", "./..."]

FROM base AS build
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/secunda ./cmd/api

FROM scratch AS app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/secunda /usr/local/bin/secunda
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["secunda"]
CMD ["serve"]
