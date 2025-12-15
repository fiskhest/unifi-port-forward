# Build
FROM --platform=linux/amd64 golang 

WORKDIR /build
COPY . . 
RUN go build -v -o /app/port-forward
WORKDIR /app
CMD ["./port-forward"]