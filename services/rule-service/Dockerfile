FROM golang:1.25-alpine AS build

WORKDIR /src
# Copy the rule-service module files
COPY services/rule-service/go.mod services/rule-service/go.sum ./
RUN go mod download
COPY services/rule-service/ .
# Copy repo-root migrations so //go:embed can reach them
COPY migrations/ migrations/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/rule-service ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/rule-service /app/rule-service
EXPOSE 8090
ENTRYPOINT ["/app/rule-service"]
