FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /grctld ./grctld

FROM gcr.io/distroless/static-debian12
COPY --from=builder /grctld /grctld
EXPOSE 4225
ENTRYPOINT ["/grctld"]
CMD ["--log-level", "debug"]
