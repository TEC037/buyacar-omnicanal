# BuyaCAR Omnicanal — Motor Go Multi-Stage Build
FROM golang:1.23-alpine AS builder

WORKDIR /src

# Copiar dependencias y fuentes
COPY app_build/go.mod ./
# Si en el futuro hay go.sum se incluirá; por ahora go standard pura
COPY app_build/ ./

# Compilar binario estático optimizado para Linux
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/buyacar ./cmd/buyacar

# Imagen final ultra ligera (< 30 MB)
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata
ENV TZ=America/Bogota

WORKDIR /app

# Copiar el binario compilado y fixtures si aplican
COPY --from=builder /bin/buyacar /app/buyacar
COPY production_artifacts /app/production_artifacts

# Puerto configurable (Railway asigna automáticamente PORT)
ENV PORT=8080
ENV BUYACAR_OPERATING_MODE=SIMULATION

EXPOSE 8080

CMD ["sh", "-c", "/app/buyacar --server -port ${PORT}"]
