FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY . .

ARG APP_FILE

RUN echo "Building target: $APP_FILE" && \
    CGO_ENABLED=0 go build -o binary -v $APP_FILE

FROM alpine:latest

WORKDIR /root/
COPY --from=builder /app/binary .

EXPOSE 8080 8081 6060 7777

CMD ["./binary"]
