FROM golang:1.15-alpine AS builder

# Run Docker container as non-root, borrowed from (Apache License 2.0):
# https://github.com/controlplaneio/kubesec/blob/master/Dockerfile.scratch
RUN echo "application:x:13456:13456:application:/home/application:/sbin/nologin" > /passwd && \
    echo "application:x:13456:" > /group

RUN apk update && apk add --no-cache git 
ARG PROJECT_DIR=$GOPATH/src/github.com/ovotech/iam-service-account-controller
COPY . ${PROJECT_DIR}
WORKDIR ${PROJECT_DIR}

RUN GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -ldflags="-w -s" -o /go/bin/iam-service-account-controller

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /go/bin/iam-service-account-controller /go/bin/iam-service-account-controller
COPY --from=builder /passwd /group /etc/
USER application
ENTRYPOINT ["/go/bin/iam-service-account-controller"]