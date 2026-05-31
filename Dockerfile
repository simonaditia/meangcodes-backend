FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o server .

FROM alpine:3.19
WORKDIR /app
COPY --from=build /app/server .

RUN chmod +x server

EXPOSE 3602
CMD ["./server"]